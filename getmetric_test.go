package getmetric

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func init() {
	isDebugRun = true
	// useFakeSend = true
}

func TestQueue(t *testing.T) {
	mon, _ := NewMonitoring("eu", 1)
	mon.sendPort = 9500 // debug

	for x := 0; x < 10; x++ {
		err := mon.PushMeasure("DEBUG_1", x)
		if !assert.True(t, err == nil, "push measure") {
			fmt.Println("ERROR: ", err)
			return
		}
	}

	// wait for send
	time.Sleep(time.Duration(5) * time.Second)

	// check send status
	assert.True(t, mon.GetSentCount() > 0, "sent count check")
	// mon.GetLastError()
}

func TestPerSec(t *testing.T) {

	mon, _ := NewMonitoring("eu", 5)
	// mon.sendPort = 9500 // debug

	// first run
	for x := 0; x < 1000; x++ {
		// single value
		err := mon.PushPerSecondMeasure("DEBUG_1", 1)
		if !assert.True(t, err == nil, "push per sec measure #1") {
			fmt.Println("ERROR: ", err)
			return
		}

		// multi values
		err = mon.PushPerSecondMeasures("DEBUG_7", MonValue{Name: "test_int", Value: 1},
			MonValue{Name: "test_float", Value: 0.1})
		if !assert.True(t, err == nil, "push per sec measure #2") {
			fmt.Println("ERROR: ", err)
			return
		}

		time.Sleep(time.Duration(10) * time.Millisecond)
	}

	// wait
	time.Sleep(time.Duration(1) * time.Second)

	// next run
	for x := 0; x < 500; x++ {
		// single value
		err := mon.PushPerSecondMeasure("DEBUG_1", 1)
		if !assert.True(t, err == nil, "push per sec measure") {
			fmt.Println("ERROR: ", err)
			return
		}
		// multi values
		err = mon.PushPerSecondMeasures("DEBUG_8", MonValue{Name: "test_int", Value: 1},
			MonValue{Name: "test_float", Value: 0.1})
		if !assert.True(t, err == nil, "push per sec measure #2") {
			fmt.Println("ERROR: ", err)
			return
		}

		time.Sleep(time.Duration(10) * time.Millisecond)
	}

	// wait
	time.Sleep(time.Duration(1) * time.Second)
	// dummy, to dump measurement
	_ = mon.PushPerSecondMeasure("DEBUG_1", 1)
	// multi values
	_ = mon.PushPerSecondMeasures("DEBUG_8", MonValue{Name: "test_int", Value: 1},
		MonValue{Name: "test_float", Value: 0.1})

	// wait for send
	time.Sleep(time.Duration(5) * time.Second)

	// check send status
	assert.True(t, mon.GetSentCount() > 0, "sent count check")
	// mon.GetLastError()
}
