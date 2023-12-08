// Package amqp contains AMQP API for a remote server.
package amqp

import (
	"context"
	"encoding/json"
	"time"

	amqpDriver "github.com/rabbitmq/amqp091-go"
	"github.com/vmihailenco/msgpack/v5"
	"go.k6.io/k6/js/modules"
)

const version = "v0.3.0"

// AMQP type holds connection to a remote AMQP server.
type AMQP struct {
	Version    string
	Connection *amqpDriver.Connection
	Queue      *Queue
	Exchange   *Exchange
}

// Options defines configuration options for an AMQP session.
type Options struct {
	ConnectionURL string
}

// PublishOptions defines a message payload with delivery options.
type PublishOptions struct {
	QueueName     string
	Body          string
	Headers       amqpDriver.Table
	Exchange      string
	ContentType   string
	Mandatory     bool
	Immediate     bool
	Persistent    bool
	CorrelationId string
	ReplyTo       string
	Expiration    string
	MessageId     string
	Timestamp     int64 // unix epoch timestamp in seconds
	Type          string
	UserId        string
	AppId         string
}

// ConsumeOptions defines options for use when consuming a message.
type ConsumeOptions struct {
	Consumer  string
	AutoAck   bool
	Exclusive bool
	NoLocal   bool
	NoWait    bool
	Args      amqpDriver.Table
}

// ListenerType is the message handler implemented within JavaScript.
type ListenerType func(string) error

// ListenOptions defines options for subscribing to message(s) within a queue.
type ListenOptions struct {
	Listener  ListenerType
	QueueName string
	Consumer  string
	AutoAck   bool
	Exclusive bool
	NoLocal   bool
	NoWait    bool
	Args      amqpDriver.Table
}

// GetOptions defines options for getting first message from an AMQP queue or wait some time for one if the queue is empty
type GetOptions struct {
	QueueName         string
	Consumer          string
	Exclusive         bool
	NoLocal           bool
	NoWait            bool
	Args              amqpDriver.Table
	WaitingTimeoutSec int // how long to wait for the message if the queue is empty, 0 (do not wait) by default
}

const messagepack = "application/x-msgpack"

// Start establishes a session with an AMQP server given the provided options.
func (amqp *AMQP) Start(options Options) error {
	conn, err := amqpDriver.Dial(options.ConnectionURL)
	amqp.Connection = conn
	amqp.Queue.Connection = conn
	amqp.Exchange.Connection = conn
	return err
}

// Publish delivers the payload using options provided.
func (amqp *AMQP) Publish(options PublishOptions) error {
	ch, err := amqp.Connection.Channel()
	if err != nil {
		return err
	}
	defer func() {
		_ = ch.Close()
	}()

	publishing := amqpDriver.Publishing{
		Headers:     options.Headers,
		ContentType: options.ContentType,
	}

	if options.ContentType == messagepack {
		var jsonParsedBody interface{}

		if err = json.Unmarshal([]byte(options.Body), &jsonParsedBody); err != nil {
			return err
		}

		publishing.Body, err = msgpack.Marshal(jsonParsedBody)
		if err != nil {
			return err
		}
	} else {
		publishing.Body = []byte(options.Body)
	}

	if options.Persistent {
		publishing.DeliveryMode = amqpDriver.Persistent
	}

	// Well, I guess 1970-01-01 isn't allowed now.
	if options.Timestamp != 0 {
		publishing.Timestamp = time.Unix(options.Timestamp, 0)
	}

	publishing.CorrelationId = options.CorrelationId
	publishing.ReplyTo = options.ReplyTo
	publishing.Expiration = options.Expiration
	publishing.MessageId = options.MessageId
	publishing.Type = options.Type
	publishing.UserId = options.UserId
	publishing.AppId = options.AppId

	return ch.PublishWithContext(
		context.Background(), // TODO: use vu context
		options.Exchange,
		options.QueueName,
		options.Mandatory,
		options.Immediate,
		publishing,
	)
}

// Listen binds to an AMQP queue in order to receive message(s) as they are received.
func (amqp *AMQP) Listen(options ListenOptions) error {
	ch, err := amqp.Connection.Channel()
	if err != nil {
		return err
	}
	defer func() {
		_ = ch.Close()
	}()

	msgs, err := ch.Consume(
		options.QueueName,
		options.Consumer,
		options.AutoAck,
		options.Exclusive,
		options.NoLocal,
		options.NoWait,
		options.Args,
	)
	if err != nil {
		return err
	}

	go func() {
		for d := range msgs {
			err = options.Listener(string(d.Body))
		}
	}()
	return err
}

// Get first message from an AMQP queue or wait some time for one if the queue is empty. Empty string is returned if no message.
func (amqp *AMQP) Get(options GetOptions) (string, error) {
	msg := ""

	ch, err := amqp.Connection.Channel()
	if err != nil {
		return msg, err
	}
	defer func() {
		_ = ch.Close()
	}()

	msgs, err := ch.Consume(
		options.QueueName,
		options.Consumer,
		false,
		options.Exclusive,
		options.NoLocal,
		options.NoWait,
		options.Args,
	)
	if err != nil {
		return msg, err
	}
	
	timeout := options.WaitingTimeoutSec

	if timeout <= 0 {
		timeout = 1
	}

	select {
	case m := <-msgs:
		// message received
		msg = string(m.Body)
		err := m.Ack(false)
		return msg, err
	case <-time.After(time.Duration(timeout) * time.Second):
		// timeout
		return msg, err
	}
}

func init() {
	queue := Queue{}
	exchange := Exchange{}
	generalAMQP := AMQP{
		Version:  version,
		Queue:    &queue,
		Exchange: &exchange,
	}

	modules.Register("k6/x/amqp", &generalAMQP)
	modules.Register("k6/x/amqp/queue", &queue)
	modules.Register("k6/x/amqp/exchange", &exchange)
}
