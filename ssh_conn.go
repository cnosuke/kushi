package main

import (
	"net"
	"time"

	"context"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type SSHConn struct {
	net.Conn
	Timeout           time.Duration
	KeepaliveInterval time.Duration
}

func (c *SSHConn) Read(b []byte) (int, error) {
	err := c.Conn.SetReadDeadline(time.Now().Add(c.Timeout))
	if err != nil {
		return 0, err
	}

	return c.Conn.Read(b)
}

func (c *SSHConn) Write(b []byte) (int, error) {
	err := c.Conn.SetWriteDeadline(time.Now().Add(c.Timeout))
	if err != nil {
		return 0, err
	}

	return c.Conn.Write(b)
}

func (c *SSHConn) doKeepAlive(cli *ssh.Client, ctx context.Context, parentCancel context.CancelFunc) (err error) {
	t := time.NewTicker(c.KeepaliveInterval)
	defer t.Stop()

	for {
		<-t.C
		keepAliveCtx, _ := context.WithTimeout(ctx, c.Timeout)

		resChan := make(chan bool)
		errChan := make(chan error)

		go func() {
			r, _, e := cli.Conn.SendRequest("keepalive@example.com", true, nil)
			if e != nil {
				errChan <- e
			} else {
				resChan <- r
			}
		}()

		select {
		case <-resChan:
		case err = <-errChan:
			zap.S().Error(err)
			parentCancel()
			return
		case <-keepAliveCtx.Done():
			zap.S().Warnf("KeepAlive timeout (%d seconds)", c.Timeout/time.Second)
			parentCancel()
			return
		case <-ctx.Done():
			return
		}
	}
}

func NewSSHClient(addr string, config *ssh.ClientConfig, timeout time.Duration, keepAlive time.Duration, ctx context.Context, parentCancel context.CancelFunc) (cli *ssh.Client, err error) {

	dial, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return
	}

	sshConn := &SSHConn{
		Conn:              dial,
		Timeout:           timeout,
		KeepaliveInterval: keepAlive,
	}

	cn, chans, reqs, err := ssh.NewClientConn(sshConn, addr, config)
	if err != nil {
		return
	}

	cli = ssh.NewClient(cn, chans, reqs)

	go sshConn.doKeepAlive(cli, ctx, parentCancel)

	return
}
