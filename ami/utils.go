package ami

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
)

// GetUUID returns a new UUID based on /dev/urandom (unix).
func GetUUID() (string, error) {
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return "", fmt.Errorf("open /dev/urandom error:[%v]", err)
	}
	defer f.Close()
	b := make([]byte, 16)

	_, err = f.Read(b)
	if err != nil {
		return "", err
	}
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return uuid, nil
}

func command(action string, id string, v ...interface{}) ([]byte, error) {
	if action == "" {
		return nil, errors.New("invalid Action")
	}
	return marshal(
		&struct {
			Action string `ami:"Action"`
			ID     string `ami:"ActionID, omitempty"`
			V      []interface{}
		}{Action: action, ID: id, V: v},
	)
}

func send(ctx context.Context, client Client, action, id string, v interface{}) (Response, error) {
	// log.Printf("Send %s %s", action, id)
	b, err := command(action, id, v)
	if err != nil {
		return nil, err
	}
	if err := client.Send(string(b)); err != nil {
		return nil, err
	}
	return read(ctx, client, id)
}

func ReadWaitForEventString(ctx context.Context, client Client, event string, lookfor []string) (
	Response,
	error,
) {
waitagain:
	var buffer bytes.Buffer
	for {
		input, err := client.Recv(ctx)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(input)
		bs := buffer.String()
		// _, file, no, _ := runtime.Caller(1)
		// log.Printf("Caller %s#%d", file, no)
		// log.Printf("BUFFER: %q", bs)
		if strings.HasSuffix(bs, "\r\n\r\n") {
			break
		}
	}
	bs2 := buffer.String()
	pr, err := parseResponse(bs2)

	if pr.Get("Event") == event {
		missing := false
		for _, s := range lookfor {
			if !strings.Contains(bs2, s) {
				missing = true
			}
		}
		if !missing {
			return pr, err
		}
	}
	log.Printf("Event did not match %s %s", pr.Get("Event"), event)
	// if pr.Get("Event") == "OriginateResponse" {
	// 	log.Printf("OriginateResponse\n%v\n", pr)
	// }
	goto waitagain
}

func read(ctx context.Context, client Client, action string) (Response, error) {
waitagain:
	var buffer bytes.Buffer
	for {
		input, err := client.Recv(ctx)
		if err != nil {
			return nil, err
		}
		buffer.WriteString(input)
		bs := buffer.String()
		// _, file, no, _ := runtime.Caller(1)
		// log.Printf("Caller %s#%d", file, no)
		// log.Printf("BUFFER: %q", bs)
		if strings.HasSuffix(bs, "\r\n\r\n") {
			break
		}
	}
	pr, err := parseResponse(buffer.String())
	a := pr.Get("ActionID")
	if len(pr) == 0 {
		// log.Printf("--- NO ACTIONID --- \n%v\n----------------\n", pr)
		goto waitagain
	}
	if len(pr) > 0 && a != action {
		// log.Printf("++ read found unknown action %q looking for %q \n%+v", a, action, pr)
		goto waitagain
	}
	return pr, err
}

func parseResponse(input string) (Response, error) {
	resp := make(Response)
	lines := strings.Split(input, "\r\n")
	for _, line := range lines {
		keys := strings.SplitAfterN(line, ":", 2)
		if len(keys) == 2 {
			key := strings.TrimSpace(strings.Trim(keys[0], ":"))
			value := strings.TrimSpace(keys[1])
			resp[key] = append(resp[key], value)
		} else if strings.Contains(line, "\r\n\r\n") || line == "" {
			break
		}
	}
	if strings.Contains(input, "Response: Error") {
		msg, found := resp["Message"]
		if !found {
			msg = []string{"NOT FOUND"}
		}
		return resp, fmt.Errorf("Error %v", msg)
	}
	resp["raw"] = []string{input}
	return resp, nil
}

func requestList(ctx context.Context, client Client, action, id, event, complete string, v ...interface{}) (
	[]Response,
	error,
) {
	b, err := command(action, id, v)
	if err != nil {
		return nil, err
	}
	if err := client.Send(string(b)); err != nil {
		return nil, err
	}
	response := make([]Response, 0)
	for {
		rsp, err := read(ctx, client, action)
		// log.Printf("requestList %+v %v", rsp, err)
		if err != nil {
			return nil, err
		}
		e := rsp.Get("Event")
		r := rsp.Get("Response")
		if e == event {
			response = append(response, rsp)
		} else if e == complete || r != "" && r != "Success" {
			break
		}
	}
	return response, nil
}