package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	yaml "gopkg.in/yaml.v2"
)

type binding struct {
	Src string `yaml:"src"`
	Dst string `yaml:"dst"`
}

type bindingsCache struct {
	bindingList []*binding
	etag        string
	mu          sync.RWMutex
	url         string
	interval    int
	cancel      context.CancelFunc
}

func (c *bindingsCache) Read() []*binding {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.bindingList
}

func (c *bindingsCache) CompareEtag(etag string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.etag == etag
}

func (c *bindingsCache) Update(b []*binding, etag string) {
	c.mu.Lock()
	c.bindingList = b
	c.etag = etag
	c.mu.Unlock()

	return
}

func (c *bindingsCache) Watch() {
	go func() {
		for {
			c.Fetch()
			time.Sleep(time.Duration(c.interval) * time.Second)
		}
	}()
}

func (c *bindingsCache) Fetch() {
	if strings.HasPrefix(c.url, "file") {
		c.fetchLocalFile()
	} else {
		c.fetchHttp()
	}
}

func (c *bindingsCache) fetchLocalFile() {
	bytes, err := ioutil.ReadFile(strings.TrimPrefix(c.url, "file://"))
	if err != nil {
		zap.S().Error(err)
		return
	}

	hash := md5.Sum(bytes)
	md5Hash := hex.EncodeToString(hash[:])
	if c.CompareEtag(md5Hash) {
		return
	}

	var b []*binding
	err = yaml.Unmarshal(bytes, &b)
	if err != nil {
		zap.S().Error(err)
		return
	}

	c.Update(b, md5Hash)
	zap.S().Infow("Binding list updated", "url", c.url, "md5", md5Hash, "bindings", b)
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *bindingsCache) fetchHttp() {
	req, _ := http.NewRequest("GET", c.url, nil)
	req.Header.Set("If-None-Match", fmt.Sprintf("\"%s\"", c.etag))
	cli := http.Client{Timeout: 5 * time.Second}
	res, err := cli.Do(req)

	if err != nil {
		zap.S().Error(err)
		return
	}

	switch res.StatusCode {
	case 304:
		// Do nothing because response is `304 Not Modified`.
	case 200:
		etag := res.Header.Get("ETag")
		body, err := ioutil.ReadAll(res.Body)
		defer res.Body.Close()
		if err != nil {
			zap.S().Error(err)
			return
		}

		var b []*binding
		err = yaml.Unmarshal(body, &b)
		if err != nil {
			zap.S().Error(err)
			return
		}

		c.Update(b, etag)
		zap.S().Infow("Binding list updated", "url", c.url, "etag", etag, "bindings", b)
		if c.cancel != nil {
			c.cancel()
		}
	default:
		zap.S().Warnw("Unknown GET bindings response", "url", c.url, "status", res.Status)
	}
}

func NewBindingsCache(url string, interval int) *bindingsCache {
	b := bindingsCache{
		url:      url,
		interval: interval,
	}
	b.Fetch()

	return &b
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
