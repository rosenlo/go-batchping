package bping

import (
	"log"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"time"
)

type MetricValue struct {
	Endpoint  string      `json:"endpoint"`
	Metric    string      `json:"metric"`
	Value     interface{} `json:"value"`
	Step      int64       `json:"step"`
	Type      string      `json:"counterType"`
	Tags      string      `json:"tags"`
	Timestamp int64       `json:"timestamp"`
}

type TransferResponse struct {
	Message string
	Total   int
	Invalid int
	Latency int64
}

type Transfer struct {
	addr string

	rpcClient *rpc.Client

	timeout time.Duration
}

func NewTransfer(addr string, timeout time.Duration) (*Transfer, error) {
	jsonrpc, err := JsonRpcClient("tcp", addr, time.Second)
	if err != nil {
		return nil, err
	}

	return &Transfer{
		rpcClient: jsonrpc,
		addr:      addr,
		timeout:   timeout,
	}, nil
}

func JsonRpcClient(network, address string, timeout time.Duration) (*rpc.Client, error) {
	conn, err := net.DialTimeout(network, address, timeout)
	if err != nil {
		return nil, err
	}
	return jsonrpc.NewClient(conn), err
}

func (t *Transfer) Close() {
	if t.rpcClient != nil {
		t.rpcClient.Close()
	}
}

func (t *Transfer) Send(args []*MetricValue, reply *TransferResponse) error {
	done := make(chan error)
	go func() {
		done <- t.rpcClient.Call("Transfer.Update", args, reply)
	}()
	select {
	case err := <-done:
		if err != nil {
			log.Printf("[error] rpc call with error %v", err)
		}
	case <-time.After(t.timeout):
		log.Printf("[error] rpc call reach timeout: %s", t.timeout)
	}
	return nil
}
