package batchping

import (
	"log"
	"runtime/debug"
	"testing"
	"time"
)

func AssertNoError(t *testing.T, err error) {
	if err != nil {
		t.Errorf("Expected No Error but got %s, Stack:\n%s",
			err, string(debug.Stack()))
	}
}

func AssertError(t *testing.T, err error, info string) {
	if err == nil {
		t.Errorf("Expected Error but got %s, %s, Stack:\n%s",
			err, info, string(debug.Stack()))
	}
}

func TestMutilAddrs(t *testing.T) {
	type input struct {
		privileged bool
		addrs      []string
	}
	tests := []input{
		{
			false,
			[]string{
				"8.8.8.8",
				"8.8.4.4",
				"114.114.114.114",
				"::1",
			},
		},
	}
	for i := 0; i < len(tests); i++ {
		bp, err := New(tests[i].privileged)
		AssertNoError(t, err)

		bp.Count = 5
		bp.OnFinish = func(mapStat map[string]*Statistics) {
			for _, stat := range mapStat {
				t.Log(stat)
			}
		}

		err = bp.Run(tests[i].addrs)
		AssertNoError(t, err)
	}
}

func TestTimeoutRetry(t *testing.T) {
	type input struct {
		privileged bool
		addrs      []string
	}
	tests := []input{
		{
			false,
			[]string{
				"127.0.0.1",
				"127.0.0.2", // not exist
			},
		},
	}
	for i := 0; i < len(tests); i++ {
		var addrs []string
		bp, err := New(tests[i].privileged)
		AssertNoError(t, err)

		addrs = tests[i].addrs

		bp.Count = 3
		bp.Timeout = time.Second * 5
		bp.OnFinish = func(mapStat map[string]*Statistics) {
			addrs = []string{}
			for _, stat := range mapStat {
				t.Log(stat)
				if stat.PacketsRecv != stat.PacketsSent {
					addrs = append(addrs, stat.Addr)
				}
			}
		}

		retry := 3
		err = bp.Run(addrs)
		if err == nil {
			return
		}

		log.Printf("[error] %v", err)

		for i := 0; i < retry; i++ {
			log.Printf("[debug] retry: %v", addrs)
			if len(addrs) != 0 {
				err := bp.Run(addrs)
				if err == nil {
					break
				}
				t.Logf("[error] %v", err)
			}
		}

	}
}

func TestNoAddrs(t *testing.T) {
	type input struct {
		privileged bool
		addrs      []string
	}
	tests := []input{
		{
			false,
			[]string{},
		},
	}
	for i := 0; i < len(tests); i++ {
		bp, err := New(tests[i].privileged)
		AssertNoError(t, err)

		err = bp.Run(tests[i].addrs)
		AssertError(t, err, "missing addrs")
	}
}
