package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/signal"
	"os/user"
	"path"
	"syscall"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"golang.org/x/crypto/ssh"

	"github.com/Luzifer/go_helpers/v2/env"
	"github.com/Luzifer/rconfig/v2"
)

var (
	cfg = struct {
		DebugRemote          bool     `flag:"debug-remote" default:"false" description:"Send remote stderr local terminal"`
		IdentityFile         string   `flag:"identity-file,i" vardefault:"ssh_key" description:"Identity file to use for connecting to the remote"`
		IdentityFilePassword string   `flag:"identity-file-password" default:"" description:"Password for the identity file"`
		LocalAddr            string   `flag:"local-addr,l" default:"" description:"Local address / port to forward" validate:"nonzero"`
		LogLevel             string   `flag:"log-level" default:"info" description:"Log level (debug, info, warn, error, fatal)"`
		RemoteHost           string   `flag:"remote-host" default:"" description:"Remote host and port in format host:port" validate:"nonzero"`
		RemoteCommand        string   `flag:"remote-command" default:"" description:"Remote command to execute after connect"`
		RemoteListen         string   `flag:"remote-listen" default:"localhost:0" description:"Address to listen on remote (port is available in script)"`
		RemoteScript         string   `flag:"remote-script" default:"" description:"Bash script to push and execute (overwrites remote-command)"`
		RemoteUser           string   `flag:"remote-user" vardefault:"remote_user" description:"User to use to connect to remote host"`
		Vars                 []string `flag:"var,v" default:"" description:"Environment variables to pass to the script (Format VAR=value)"`
		VersionAndExit       bool     `flag:"version" default:"false" description:"Prints current version and exits"`
	}{}

	running = true

	version = "dev"
)

func forward(remoteConn net.Conn) {
	defer remoteConn.Close()

	localConn, err := net.Dial("tcp", cfg.LocalAddr)
	if err != nil {
		log.WithError(err).Error("Unable to connect to local address")
		return
	}
	defer localConn.Close()

	copyConn := func(w, r net.Conn, wg chan struct{}) {
		_, err := io.Copy(w, r)
		if err != nil {
			log.WithError(err).Debug("IO copy caused an error, terminating connection")
		}
		wg <- struct{}{}
	}

	var wg = make(chan struct{}, 2)

	go copyConn(localConn, remoteConn, wg)
	go copyConn(remoteConn, localConn, wg)

	<-wg
}

func genDefaults() map[string]string {
	defs := map[string]string{}

	if userHome, err := os.UserHomeDir(); err == nil {
		defs["ssh_key"] = path.Join(userHome, ".ssh", "id_rsa")
	}

	if user, err := user.Current(); err == nil {
		defs["remote_user"] = user.Username
	}

	return defs
}

func init() {
	rconfig.SetVariableDefaults(genDefaults())

	rconfig.AutoEnv(true)
	if err := rconfig.ParseAndValidate(&cfg); err != nil {
		log.Fatalf("Unable to parse commandline options: %s", err)
	}

	if cfg.VersionAndExit {
		fmt.Printf("shareport %s\n", version)
		os.Exit(0)
	}

	if l, err := log.ParseLevel(cfg.LogLevel); err != nil {
		log.WithError(err).Fatal("Unable to parse log level")
	} else {
		log.SetLevel(l)
	}
}

func loadPrivateKey() (ssh.AuthMethod, error) {
	kf, err := ioutil.ReadFile(cfg.IdentityFile)
	if err != nil {
		return nil, errors.Wrap(err, "Unable to read key file")
	}

	pk, err := signerFromPem(kf, []byte(cfg.IdentityFilePassword))
	return ssh.PublicKeys(pk), errors.Wrap(err, "Unable to parse private key")
}

func main() {
	sigC := make(chan os.Signal)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)

	privateKey, err := loadPrivateKey()
	if err != nil {
		log.WithError(err).Fatal("Unable to load key")
	}

	config := &ssh.ClientConfig{
		User:            cfg.RemoteUser,
		Auth:            []ssh.AuthMethod{privateKey},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Connect to remote
	client, err := ssh.Dial("tcp", cfg.RemoteHost, config)
	if err != nil {
		log.WithError(err).Fatal("Unable to connect to remote host")
	}

	// Open port for us to listen on
	remoteListener, err := client.Listen("tcp", cfg.RemoteListen)
	if err != nil {
		log.WithError(err).Fatal("Unable to listen for connection")
	}
	defer remoteListener.Close()

	_, port, err := net.SplitHostPort(remoteListener.Addr().String())
	if err != nil {
		log.WithError(err).Fatal("Unable to get port of remote listen socket")
	}

	log.WithField("port", port).Debug("Remote port established")

	go func() {
		for running {
			remoteConn, err := remoteListener.Accept()
			if err != nil {
				log.WithError(err).Error("Unable to accept remote connection")
				continue
			}

			go forward(remoteConn)
		}
	}()

	// Initialize script
	var scriptIn = new(bytes.Buffer)
	fmt.Fprintln(scriptIn, "set -euxo pipefail")

	// Create remote script session
	session, err := client.NewSession()
	if err != nil {
		log.WithError(err).Fatal("Unable to open remote session")
	}
	defer session.Close()

	envVars := env.ListToMap(cfg.Vars)
	envVars["PORT"] = port
	envVars["LISTEN"] = remoteListener.Addr().String()

	for k, v := range envVars {
		fmt.Fprintf(scriptIn, "export %s=%q\n", k, v)
	}

	switch {
	case cfg.RemoteScript != "":
		script, err := ioutil.ReadFile(cfg.RemoteScript)
		if err != nil {
			log.WithError(err).Fatal("Unable to load remote-script")
		}
		scriptIn.Write(script)

	case cfg.RemoteCommand != "":
		fmt.Fprintf(scriptIn, "exec %s", cfg.RemoteCommand)

	default:
		log.Fatal("Neither remote-command nor remote-script specified")
	}

	if cfg.DebugRemote {
		session.Stderr = os.Stderr
	} else {
		session.Stderr = ioutil.Discard
	}

	session.Stdin = scriptIn
	session.Stdout = os.Stdout

	if err := session.Start("/bin/bash -euxo pipefail"); err != nil {
		log.WithError(err).Fatal("Unable to spawn remote command")
	}

	go func() {
		if err := session.Wait(); err != nil {
			log.WithError(err).Error("Remote process caused an error")
		}
		sigC <- syscall.SIGINT
	}()

	for {
		select {
		case <-sigC:
			log.Info("Signal triggered, shutting down")
			if err := session.Signal(ssh.SIGHUP); err != nil {
				log.WithError(err).Error("Unable to send TERM signal to remote process")
			}
			running = false
			return
		}
	}
}
