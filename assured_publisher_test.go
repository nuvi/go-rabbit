package rabbit_test

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/brettallred/go-rabbit"
	"github.com/streadway/amqp"
	"github.com/stretchr/testify/assert"
)

func TestPublishAssured(t *testing.T) {
	var subscriber = rabbit.Subscriber{
		Concurrency: 5,
		Durable:     true,
		Exchange:    "events_test",
		Queue:       "test.assuredpublishsample.event.created",
		RoutingKey:  "assuredpublishsample.event.created",
	}
	assert := assert.New(t)

	message := "Test Message"
	publisher := rabbit.NewAssuredPublisher(make(chan bool))
	err := rabbit.CreateQueue(publisher.GetChannel(), &subscriber)
	assert.Nil(err)
	ok := publisher.Publish(message, &subscriber)

	var result string
	result, _ = rabbit.Pop(&subscriber)
	assert.Equal(message, result)
	assert.True(ok)
}

func TestPublishWithExplicitWaiting(t *testing.T) {
	var subscriber = rabbit.Subscriber{
		Concurrency: 5,
		Durable:     true,
		Exchange:    "events_test",
		Queue:       "test.assuredpublishsampleexpl.event.created",
		RoutingKey:  "publishsampleexp.event.created",
	}
	assert := assert.New(t)

	cancel := make(chan bool)
	publisher := rabbit.NewAssuredPublisher(cancel)
	publisher.SetExplicitWaiting()
	err := rabbit.CreateQueue(publisher.GetChannel(), &subscriber)
	assert.Nil(err)

	publisher.GetChannel().QueueDelete(subscriber.Queue, true, false, false)

	messagesMap := map[string]bool{}
	doneReading := make(chan bool)
	messagesRead := 0
	doneReadingIsClosed := false
	lock := sync.Mutex{}

	subscriberHandler := func(delivery amqp.Delivery) bool {
		lock.Lock()
		defer lock.Unlock()
		messagesMap[string(delivery.Body)] = true
		messagesRead++
		if len(messagesMap) == 10000 && !doneReadingIsClosed {
			close(doneReading)
			doneReadingIsClosed = true
		}
		return true
	}
	rabbit.Register(subscriber, subscriberHandler)
	rabbit.StartSubscribers()
	defer rabbit.CloseSubscribers()

	for i := 0; i < 10000; i++ {
		if i != 0 && i%999 == 0 {
			// kill the connection
			publisher.GetChannel().ExchangeDelete(subscriber.Exchange, true, true)
			publisher.GetChannel().ExchangeDeclare(subscriber.Exchange, "topic", false, false, false, false, nil)
		}
		ok := publisher.Publish(fmt.Sprintf("%d", i), &subscriber)
		assert.True(ok)
	}
	done := make(chan bool)
	go func() {
		result := publisher.WaitForAllConfirmations()
		assert.True(result)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(cancel)
		assert.Fail("Timeout while waiting for confirmations")
	}

	select {
	case <-doneReading:
	case <-time.After(10 * time.Second):
		close(cancel)
		assert.Fail(fmt.Sprintf("Timeout while reading messages. Messages read: %d", messagesRead))
	}
	assert.Len(messagesMap, 10000)
}

func TestDisableRepublishing(t *testing.T) {
	var subscriber = rabbit.Subscriber{
		Concurrency: 5,
		Durable:     true,
		Exchange:    "events_test",
		Queue:       "test.assuredpublishsampleexpl.event.created",
		RoutingKey:  "publishsampleexp.event.created",
	}
	assert := assert.New(t)

	cancel := make(chan bool)
	publisher := rabbit.NewAssuredPublisher(cancel)
	publisher.SetExplicitWaiting()
	publisher.DisableRepublishing()
	handlerCalledMap := map[uint64]int{}
	publisher.SetConfirmationHandler(func(confirmation amqp.Confirmation, arg interface{}) {
		handlerCalledMap[confirmation.DeliveryTag]++
	})
	err := rabbit.CreateQueue(publisher.GetChannel(), &subscriber)
	assert.Nil(err)

	publisher.GetChannel().QueueDelete(subscriber.Queue, true, false, false)

	for i := 0; i < 10; i++ {
		ok := publisher.Publish(fmt.Sprintf("%d", i), &subscriber)
		assert.True(ok)
		publisher.Close()
	}
	log.Println("Waiting for all confirmations")
	publisher.WaitForAllConfirmations()
	log.Println("Done waiting for all confirmations")
	assert.Len(handlerCalledMap, 1)
	assert.Equal(10, handlerCalledMap[1])
}
