package client

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"

	"opensvc.com/opensvc/config"

	"golang.org/x/net/http2"
)

type (
	// H2 is the agent HTTP/2 api client struct
	H2 struct {
		Requester
		Client http.Client
		URL    string
	}
)

const (
	h2UDSPrefix  = "http:///"
	h2InetPrefix = "https://"
)

var (
	wellKnowH2UDSURLS  = []string{"http", "http://", "/"}
	wellKnowH2InetURLS = []string{"https", "https://"}
)

func (t H2) String() string {
	return fmt.Sprintf("H2 %s", t.URL)
}

func defaultH2UDSPath() string {
	return filepath.FromSlash(fmt.Sprintf("%s/lsnr/h2.sock", config.Viper.GetString("paths.var")))
}

func newH2UDS(c Config) (H2, error) {
	var url string
	if c.url == "" {
		url = defaultH2UDSPath()
	} else {
		url = c.url
	}
	r := &H2{}
	t := &http2.Transport{
		AllowHTTP: true,
		DialTLS: func(network, addr string, cfg *tls.Config) (net.Conn, error) {
			return net.Dial("unix", url)
		},
	}
	r.URL = "http://localhost"
	r.Client = http.Client{Transport: t}
	return *r, nil
}

func newH2Inet(c Config) (H2, error) {
	r := &H2{}
	cer, err := tls.LoadX509KeyPair(c.clientCertificate, c.clientKey)
	if err != nil {
		return *r, err
	}
	t := &http2.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: c.insecureSkipVerify,
			Certificates:       []tls.Certificate{cer},
		},
	}
	r.URL = c.url
	r.Client = http.Client{Transport: t}
	return *r, nil
}

func (t H2) newRequest(method string, r Request) (*http.Request, error) {
	jsonStr, _ := json.Marshal(r.Options)
	body := bytes.NewBuffer(jsonStr)
	req, err := http.NewRequest("GET", t.URL+"/"+r.Action, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("o-node", r.Node)
	return req, nil
}

func (t H2) doReq(method string, r Request) (*http.Response, error) {
	req, err := t.newRequest(method, r)
	if err != nil {
		return nil, err
	}
	resp, err := t.Client.Do(req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (t H2) doReqReadResponse(method string, r Request) ([]byte, error) {
	resp, err := t.doReq(method, r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Get implements the Get interface for the H2 protocol
func (t H2) Get(r Request) ([]byte, error) {
	return t.doReqReadResponse("GET", r)
}

// Post implements the Post interface for the H2 protocol
func (t H2) Post(r Request) ([]byte, error) {
	return t.doReqReadResponse("POST", r)
}

// Put implements the Put interface for the H2 protocol
func (t H2) Put(r Request) ([]byte, error) {
	return t.doReqReadResponse("PUT", r)
}

// Delete implements the Delete interface for the H2 protocol
func (t H2) Delete(r Request) ([]byte, error) {
	return t.doReqReadResponse("DELETE", r)
}

// GetStream returns a chan of raw json messages
func (t H2) GetStream(r Request) (chan []byte, error) {
	req, err := t.newRequest("GET", r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := t.Client.Do(req)
	if err != nil {
		return nil, err
	}
	q := make(chan []byte, 1000)
	if err != nil {
		return nil, err
	}
	go getServerSideEvents(q, resp)
	return q, nil
}

func getServerSideEvents(q chan<- []byte, resp *http.Response) error {
	br := bufio.NewReader(resp.Body)
	delim := []byte{':', ' '}
	defer resp.Body.Close()
	defer close(q)
	for {
		bs, err := br.ReadBytes('\n')

		if err != nil && err != io.EOF {
			return err
		}

		if len(bs) < 2 {
			continue
		}

		spl := bytes.Split(bs, delim)

		if len(spl) < 2 {
			continue
		}

		switch string(spl[0]) {
		case "data":
			b := bytes.TrimLeft(bs, "data: ")
			q <- b
		}
		if err == io.EOF {
			break
		}
	}
	return nil
}