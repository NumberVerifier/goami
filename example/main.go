package main

import (
	"context"
	"errors"
	"flag"
	"github.com/NumberVerifier/goami/ami"
	"log"
	"sync"
	"time"
)

var (
	username = flag.String("username", "user", "AMI username")
	secret   = flag.String("secret", "secret", "AMI secret")
	host     = flag.String("host", "127.0.0.1:5038", "AMI host address")
)

func main() {
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// actx, acancel := context.WithTimeout(ctx, 5*time.Second)
	// defer acancel()

	asterisk, err := NewAsterisk(ctx, *host, *username, *secret)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("global actionid %s", asterisk.uuid)
	defer asterisk.Logoff(ctx)

	// pctx, pcancel := context.WithTimeout(ctx, 3*time.Second)
	// defer pcancel()
	// peers, err := asterisk.ListCommands(pctx)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Printf("commands: %v\n", peers)

	g, err := ami.GetUUID()
	if err != nil {
		log.Fatal(err)
	}

	// reventsctx, reventscancel := context.WithTimeout(ctx, 5*time.Second)
	// defer reventscancel()
	// _, err = ami.EventFlow(reventsctx, asterisk.socket, g, "on")
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// phone := "+18004444444"
	phone := "+17148180214"
	callerid := "+12132133000"
	octx, ocancel := context.WithTimeout(ctx, 15*time.Second)
	defer ocancel()
	var od ami.OriginateData
	od.Async = "yes"
	od.Application = "Hangup"
	od.CallerID = callerid
	od.Channel = "PJSIP/" + phone + "@flow"

	oresp, err := ami.Originate(octx, asterisk.socket, g, od)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Originate response:\n%v", oresp)

	ectx, ecancel := context.WithTimeout(ctx, 20*time.Second)
	defer ecancel()
	dialresp, err := ami.ReadWaitForEventString(ectx, asterisk.socket, "DialBegin", []string{callerid, phone})
	if err != nil {
		log.Fatalf("Error getting DialBegin %v", err)
	}
	log.Printf("Dialresponse:\n%v\n", dialresp)
	channel := dialresp.Get("DestChannel")
	log.Printf("Channel: %s", channel)

	ectx2, ecancel2 := context.WithTimeout(ctx, 20*time.Second)
	defer ecancel2()
	earesp, eaerr := ami.EventsAction(ectx2, asterisk.socket, g)
	if eaerr != nil {
		log.Printf("Failed to get response %v", eaerr)
		hresp, herr := ami.Hangup(ctx, asterisk.socket, g, channel, "1")
		if herr != nil {
			log.Fatalf("Failed to hangup caller %v", herr)
		}
		log.Printf("Hresp %v", hresp)
	} else {
		log.Printf("Response %v", earesp)
	}

	// dialresp, err := ami.ReadWaitForEventString(ectx, asterisk.socket, "DialBegin", []string{callerid, phone})
	// if err != nil {
	// 	log.Fatalf("Error getting DialBegin %v", err)
	// }
	// log.Printf("Dialresponse:\n%v\n", dialresp)
	// channel := dialresp.Get("DestChannel")
	// log.Printf("Channel: %s", channel)
	//
	// endctx, endcancel := context.WithTimeout(ctx, 20*time.Second)
	// defer endcancel()
	// endResponse, enderr := ami.ReadWaitForEventString(endctx, asterisk.socket, "DialEnd", []string{channel})
	// if enderr != nil {
	// 	log.Fatalf("Error getting DialEnd %v", enderr)
	// }
	// log.Printf("---- End Response\n%v\n", endResponse)
	// dialstatus := endResponse.Get("DialStatus")
	// log.Printf("DialStatus: %s", dialstatus)
	// if dialstatus == "ANSWER" {
	// 	// Wait again
	// 	wendctx, wendcancel := context.WithTimeout(ctx, 20*time.Second)
	// 	defer wendcancel()
	// 	wendResponse, wenderr := ami.ReadWaitForEventString(wendctx, asterisk.socket, "DialEnd", []string{channel})
	// 	if wenderr != nil {
	// 		log.Fatal("Failed to get dialend from answer %v", wenderr)
	// 	}
	// 	log.Printf("DialEnd after answer\n%v\n", wendResponse)
	// }

	// eventResponse, err := ami.EventsAction(ectx, asterisk.socket, g)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// log.Printf("Event: %v", eventResponse)
}

type Asterisk struct {
	socket *ami.Socket
	uuid   string

	events chan ami.Response
	stop   chan struct{}
	wg     sync.WaitGroup
}

// NewAsterisk initializes the AMI socket with a login and capturing the events.
func NewAsterisk(ctx context.Context, host string, username string, secret string) (*Asterisk, error) {
	socket, err := ami.NewSocket(ctx, host)
	if err != nil {
		return nil, err
	}
	uuid, err := ami.GetUUID()
	if err != nil {
		return nil, err
	}
	const events = "system,call,all,user"
	err = ami.Login(ctx, socket, username, secret, events, uuid)
	if err != nil {
		return nil, err
	}
	as := &Asterisk{
		socket: socket,
		uuid:   uuid,
		events: make(chan ami.Response),
		stop:   make(chan struct{}),
	}
	as.wg.Add(1)
	go as.run(ctx)
	return as, nil
}

// Logoff closes the current session with AMI.
func (as *Asterisk) Logoff(ctx context.Context) error {
	close(as.stop)
	as.wg.Wait()

	return ami.Logoff(ctx, as.socket, as.uuid)
}

// Events returns an channel with events received from AMI.
func (as *Asterisk) Events() <-chan ami.Response {
	return as.events
}

func (as *Asterisk) ListCommands(ctx context.Context) ([]ami.Response, error) {
	resp, err := ami.ListCommands(ctx, as.socket, as.uuid)
	switch {
	case err != nil:
		return nil, err
	case len(resp) == 0:
		return nil, errors.New("no response")
	default:
		return []ami.Response{resp}, nil

	}
}

func (as *Asterisk) run(ctx context.Context) {
	defer as.wg.Done()
	for {
		select {
		case <-as.stop:
			return
		case <-ctx.Done():
			return
		default:
			time.Sleep(1 * time.Second)
			// events, err := ami.Events(ctx, as.socket)
			// if err != nil {
			// 	log.Printf("AMI events failed: %v\n", err)
			// 	return
			// }
			// as.events <- events
		}
	}
}
