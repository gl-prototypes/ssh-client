package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/user"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
)

func tryRSA(usr *user.User) ssh.Signer {
	key, err := ioutil.ReadFile(fmt.Sprintf("%s/.ssh/id_rsa", usr.HomeDir))
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil
	}
	return signer
}

func tryDSA(usr *user.User) ssh.Signer {
	key, err := ioutil.ReadFile(fmt.Sprintf("%s/.ssh/id_dsa", usr.HomeDir))
	if err != nil {
		return nil
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil
	}
	return signer
}

func main() {
	flag.Parse()

	u, err := user.Current()
	if err != nil {
		panic(err)
	}

	config := &ssh.ClientConfig{
		User: u.Username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(
				tryRSA(u),
				tryDSA(u),
			),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := flag.Arg(0)
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "22"
	}

	client, err := ssh.Dial("tcp", net.JoinHostPort(host, port), config)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	//session.Stdout = os.Stdout
	//session.Stderr = os.Stderr

	cmd := flag.Args()[1:]
	if len(cmd) > 2 && cmd[0] == "--" {
		cmd = cmd[1:]
	}

	if len(cmd) > 0 {
		if err := session.Start(strings.Join(cmd, " ")); err != nil {
			panic(err)
		}
	} else {
		modes := ssh.TerminalModes{
			ssh.ECHO:          0,     // disable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}
		term := os.Getenv("TERM")
		if term == "" {
			term = "xterm"
		}
		width, height, err := terminal.GetSize(0)
		if err != nil {
			panic(err)
		}
		if err := session.RequestPty(term, height, width, modes); err != nil {
			panic(err)
		}
		// oldState, err := terminal.MakeRaw(0)
		// if err != nil {
		// 	panic(err)
		// }
		// defer terminal.Restore(0, oldState)
		// TODO: handle SIGWINCH
		if err := session.Shell(); err != nil {
			panic(err)
		}
	}

	forward := os.Getenv("SSH_REMOTE_FORWARD") // example: localhost:9000->localhost:3000
	parts := strings.Split(forward, "->")
	if len(parts) == 2 {
		listenAddr, err := net.ResolveTCPAddr("tcp", parts[0])
		if err != nil {
			panic(err)
		}
		l, err := client.ListenTCP(listenAddr)
		if err != nil {
			panic(err)
		}
		go func() {
			for {
				ch, err := l.Accept()
				if err != nil {
					panic(err)
				}
				go func() {
					c, err := net.Dial("tcp", parts[1])
					if err != nil {
						log.Println(err)
						ch.Close()
						return
					}
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(ch, c)
					}()
					go func() {
						defer ch.Close()
						defer c.Close()
						io.Copy(c, ch)
					}()
				}()
			}

		}()
	}

	if err := session.Wait(); err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			os.Exit(exitErr.ExitStatus())
		} else {
			panic(err)
		}
	}

}
