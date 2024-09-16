package shared

import (
	amqp "github.com/rabbitmq/amqp091-go"
)

type RMQueue struct {
	Connection *amqp.Connection
	Channel    *amqp.Channel
	Queue      amqp.Queue
	Timeout    int
}

func NewRMQueue(Url string, QueueName string) (*RMQueue, error) {
	q := &RMQueue{}
	var err error
	q.Connection, err = amqp.Dial(Url)
	if err != nil {
		return q, err
	}
	q.Channel, err = q.Connection.Channel()
	if err != nil {
		return q, err
	}
	q.Queue, err = q.Channel.QueueDeclare(QueueName, false, false, false, false, nil)
	if err != nil {
		return q, err
	}
	return q, nil
}

func (q *RMQueue) Close() {
	q.Channel.Close()
	q.Connection.Close()
}

func (q *RMQueue) Publish(body []byte) error {
	err := q.Channel.Publish("", q.Queue.Name, false, false, amqp.Publishing{Body: body})
	if err != nil {
		return err
	}
	return nil
}

func (q *RMQueue) Consume() (<-chan amqp.Delivery, error) {
	msgs, err := q.Channel.Consume(q.Queue.Name, "", false, false, false, false, nil)
	if err != nil {
		return nil, err
	}
	return msgs, nil
}
