package main

import (
	"io"

	"context"
	"net"

	"sync"

	"github.com/k0kubun/pp"
	"golang.org/x/crypto/ssh"
	yaml "gopkg.in/yaml.v2"
)

var gistURL = ""

type binding struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

func NewBindingList() (list []*binding, err error) {
	//res, err := http.Get(gistURL)
	//if err != nil {
	//	return
	//}
	//
	//body, err := ioutil.ReadAll(res.Body)
	//defer res.Body.Close()
	//if err != nil {
	//	return
	//}

	str := `
- src: localhost:8080
  dst: example.com:80
- src: localhost:4443
  dst: example.com:443
`

	body := []byte(str)

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
		pp.Println(err.Error())
	}

	return
}

func (b *binding) handle(cli *ssh.Client, ctx context.Context, cancelFunc context.CancelFunc, sessWg *sync.WaitGroup) {
	lis, err := net.Listen("tcp", b.Src)

	go func(lis net.Listener, wg *sync.WaitGroup) {
		<-ctx.Done()
		pp.Printf("Close %s\n", b.Src)
		lis.Close()
		wg.Done()
	}(lis, sessWg)

	if err != nil {
		pp.Fatalln(err.Error())
		return
	}

	var wg sync.WaitGroup

	for {

		localConn, err := lis.Accept()

		if err != nil {
			pp.Println(err.Error())
			return
		}

		sshConn, err := cli.Dial("tcp", b.Dst)

		if err != nil {
			pp.Println(err.Error())
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
