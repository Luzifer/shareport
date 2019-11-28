[![Go Report Card](https://goreportcard.com/badge/github.com/Luzifer/shareport)](https://goreportcard.com/report/github.com/Luzifer/shareport)
![](https://badges.fyi/github/license/Luzifer/shareport)
![](https://badges.fyi/github/downloads/Luzifer/shareport)
![](https://badges.fyi/github/latest-release/Luzifer/shareport)
![](https://knut.in/project-status/shareport)

# Luzifer / shareport

`shareport` is a kept simple self-hosted alternative to ngrok to share local development webservers through a remote SSH connection.

The only feature supported is to forward the port: All other features like analysing, introspecting or replaying the HTTP traffic are not supported. If you need them you should go with ngrok (and its payed plan for custom domain support).

## How to use it

- Prepare your setup including webserver, SSH key and so on (see below for my setup)
- Start a webserver for your development and note the port
- Execute `shareport`: It then will
  - Create a SSH connection
  - By default listen on a random port on the remote machine
  - Execute the given script or command on the remote machine
- After you're done just quit the shareport command and it will
  - Terminate the remote script
  - Stop listening on the port
  - Close the connection

## My Setup

- The server
  - Small [Hetzner Cloud CX11 machine](https://www.hetzner.com/cloud#pricing)
  - Ubuntu 19.04 and nginx
  - Domain `knut.dev` with a [LetsEncrypt](https://letsencrypt.org/) wildcard certificate mapped to the machine
  - Extra user for for shareport with a SSH key deployed
  - A directory in users home to house nginx server configuration
  - `sudoers` file to allow `systemctl reload nginx.service` on the unprivileged user
- The script: See [`example/remote-script.bash`](example/remote-script.bash)

In this setup Ubuntu **19.04** is a quite important part: For the script to properly being shut down the SSH connection needs to be able to transmit a TERM signal. This was implemented somewhere between OpenSSL 7.6 and 7.9 and Ubuntu 19.04 is the first version to ship OpenSSL 7.9. Every other Linux system with a recent OpenSSL version also should work.

As an example lets take a Python webserver and expose it:

```console
# echo "Ohai" >hello.txt
# python -m http.server --bind localhost 3000
Serving HTTP on 127.0.0.1 port 3000 (http://127.0.0.1:3000/) ...

# cat .env
IDENTITY_FILE=id_rsa
IDENTITY_FILE_PASSWORD=password
REMOTE_HOST=knut.dev:22
REMOTE_SCRIPT=example/remote-script.bash
REMOTE_USER=shareport

# envrun -- shareport --local-addr localhost:3000
Listening on https://4neg7kj4.knut.dev/

# curl https://4neg7kj4.knut.dev/hello.txt
Ohai
```
