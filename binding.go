package main

import (
	"context"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"sync"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	yaml "gopkg.in/yaml.v2"
)

var gistURL = "https://gist.githubusercontent.com/cnosuke/4b4322d3c9537c4265252a017b021c49/raw/ports.yml"

type binding struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

func NewBindingList() (list []*binding, err error) {
	zap.S().Infof("Get binding list from %s", gistURL)
	res, err := http.Get(gistURL)
	if err != nil {
		return
	}

	body, err := ioutil.ReadAll(res.Body)
	defer res.Body.Close()
	if err != nil {
		return
	}

	err = yaml.Unmarshal(body, &list)
	if err != nil {
		return
	}

	return
}

func (b *binding) pipe(srcConn net.Conn, destConn net.Conn, ctx context.Context, wg *sync.WaitGroup) (err error) {
	defer wg.Done()
	defer srcConn.Close()
	defer destConn.Close()

	errChan := make(chan error)
	completeChan := make(chan struct{})

	go func() {
		_, err := io.Copy(srcConn, destConn)

		if err != nil {
			errChan <- err
		}

		completeChan <- struct{}{}
	}()

	go func() {
		_, err := io.Copy(destConn, srcConn)

		if err != nil {
			errChan <- err
		}

		completeChan <- struct{}{}
	}()

	select {
	case <-ctx.Done():
	case <-completeChan:
	case err = <-errChan:
		zap.S().Error(err)
	}

	return
}

func (b *binding) handle(cli *ssh.Client, ctx context.Context, cancelFunc context.CancelFunc, sessWg *sync.WaitGroup) {
	lis, err := net.Listen("tcp", b.Src)
	if err != nil {
		zap.S().Fatal(err)
		return
	}

	go func(lis net.Listener, wg *sync.WaitGroup) {
		<-ctx.Done()
		zap.S().Infof("Closing %s", b.Src)
		lis.Close()
		wg.Done()
	}(lis, sessWg)

	var wg sync.WaitGroup

	for {

		localConn, err := lis.Accept()

		if err != nil {
			zap.S().Warn(err)
			return
		}

		sshConn, err := cli.Dial("tcp", b.Dst)

		if err != nil {
			zap.S().Warn(err)
			cancelFunc()
			return
		}

		wg.Add(1)
		go b.pipe(localConn, sshConn, ctx, &wg)

		select {
		case <-ctx.Done():
			break
		default:
		}
	}

	wg.Wait()
}
