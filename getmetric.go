package getmetric

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// debug options
var isDebugRun bool
var useFakeSend bool

func init() {
	useFakeSend = false
	isDebugRun = false
}

type Monitoring struct {
	Region string // eu, us, etc.

	running       bool // TODO: atomic
	maxQueueLen   int
	sendPeriodSec int
	batchMaxSize  int

	queueMutex *sync.RWMutex
	queue      []interface{}
	sentCount  int
	sendPort   int

	perSecQueueMutex *sync.Mutex
	perSecMap        map[string]monItemV1
}

type MonValue struct {
	Name  string      `json:"name"`
	Value interface{} `json:"value"`
}

type monItemV1 struct {
	Type   int        `json:"type"` // 1 - measure, 2 - operation, 3 - log, 4 - alert
	Code   string     `json:"code"`
	Time   time.Time  `json:"time"`
	Values []MonValue `json:"values"`
}

func NewMonitoring(hostRegion string, sendPeriodSec int) (*Monitoring, error) {
	if len(hostRegion) < 1 {
		return nil, fmt.Errorf("region is not specified")
	}

	if sendPeriodSec < 1 {
		return nil, fmt.Errorf("send per second must be greater than 0")
	}

	mon := Monitoring{Region: hostRegion}
	mon.maxQueueLen = 25000
	mon.sendPeriodSec = sendPeriodSec
	mon.batchMaxSize = 250
	mon.sentCount = 0
	mon.sendPort = -1
	mon.queueMutex = new(sync.RWMutex)
	mon.running = true
	mon.queue = make([]interface{}, 0)
	mon.perSecQueueMutex = new(sync.Mutex)
	mon.perSecMap = make(map[string]monItemV1)
	go mon.senderRoutine()
	return &mon, nil
}

func (mon *Monitoring) Stop() error {
	if !mon.running {
		return fmt.Errorf("already stopped")
	}
	mon.running = false
	return nil
}

func (mon *Monitoring) PushMeasure(code string, value interface{}) error {
	if !mon.running {
		return fmt.Errorf("not running")
	}

	if err := checkValue("", value); err != nil {
		return err
	}

	if len(code) < 1 {
		// just ignore (monitoring item is not set up)
		return nil
	}

	return mon.pushMeasureToQueue(monItemV1{
		Type: 1,
		Code: code,
		Time: time.Now().UTC(),
		Values: []MonValue{{
			Name:  "",
			Value: value,
		}}})
}

func (mon *Monitoring) PushMeasures(code string, values ...MonValue) error {
	if !mon.running {
		return fmt.Errorf("not running")
	}

	if len(code) < 1 {
		// just ignore (monitoring item is not set up)
		return nil
	}

	for _, val := range values {
		if len(val.Name) < 1 {
			return fmt.Errorf("empty value name")
		}
		if err := checkValue(val.Name, val.Value); err != nil {
			return err
		}
	}

	return mon.pushMeasureToQueue(monItemV1{
		Type:   1,
		Code:   code,
		Time:   time.Now().UTC(),
		Values: values,
	})
}

func (mon *Monitoring) PushPerSecondMeasure(code string, value interface{}) error {
	if !mon.running {
		return fmt.Errorf("not running")
	}

	if len(code) < 1 {
		// just ignore (monitoring item is not set up)
		return nil
	}

	if err := checkPerSecValue("", value); err != nil {
		return err
	}

	return mon.pushPerSecMeasureToQueue(monItemV1{
		Type: 1,
		Code: code,
		Time: time.Now().UTC(),
		Values: []MonValue{{
			Name:  "",
			Value: value,
		}}})
}

func (mon *Monitoring) PushPerSecondMeasures(code string, values ...MonValue) error {
	if !mon.running {
		return fmt.Errorf("not running")
	}

	if len(code) < 1 {
		// just ignore (monitoring item is not set up)
		return nil
	}

	for _, val := range values {
		if len(val.Name) < 1 {
			return fmt.Errorf("empty value name")
		}
		if err := checkPerSecValue(val.Name, val.Value); err != nil {
			return err
		}
	}

	return mon.pushPerSecMeasureToQueue(monItemV1{
		Type:   1,
		Code:   code,
		Time:   time.Now().UTC(),
		Values: values,
	})
}

func checkValue(name string, v interface{}) error {
	switch v.(type) {
	case int:
		return nil
	case float64:
		return nil
	case string:
		return nil
	}

	if len(name) < 1 {
		return fmt.Errorf("unknown default value type")
	}

	return fmt.Errorf("unknown value type for variable " + name)
}

func checkPerSecValue(name string, v interface{}) error {
	switch v.(type) {
	case int:
		return nil
	case float64:
		return nil
	}

	if len(name) < 1 {
		return fmt.Errorf("unknown default value type")
	}

	return fmt.Errorf("unsupported value type for variable " + name)
}

func (mon *Monitoring) pushMeasureToQueue(item monItemV1) error {
	mon.queueMutex.Lock()
	defer mon.queueMutex.Unlock()

	if len(mon.queue) > mon.maxQueueLen {
		return fmt.Errorf("monitorng queue limit reached")
	}
	mon.queue = append(mon.queue, item)
	return nil
}

func (mon *Monitoring) senderRoutine() {
	dontSleep := false
	var batchQueue []interface{}
	for mon.running {
		if !dontSleep {
			time.Sleep(time.Duration(mon.sendPeriodSec) * time.Second)
			dontSleep = false
		}

		if len(batchQueue) < 1 {
			mon.queueMutex.RLock()

			// fill batch queue
			if len(mon.queue) > mon.batchMaxSize {
				batchQueue = mon.queue[:mon.batchMaxSize]
			} else {
				batchQueue = mon.queue[:]
			}

			mon.queueMutex.RUnlock()
		}

		batchLen := len(batchQueue)
		if batchLen > 0 {
			// send batch
			err := mon.sendBatch(batchQueue)
			if err == nil {
				// release queue
				mon.queueMutex.Lock()
				mon.queue = mon.queue[batchLen:]
				if len(mon.queue) > mon.batchMaxSize {
					dontSleep = true
				}
				mon.queueMutex.Unlock()
				batchQueue = batchQueue[0:0]

				// stat
				mon.sentCount += batchLen
			} else {
				// TODO: error
			}
		}
	}
}

func (mon *Monitoring) sendBatch(batchQueue []interface{}) error {
	// prepare call
	if isDebugRun {
		fmt.Println("sending batch size", len(batchQueue))

		if useFakeSend {
			fmt.Println("fake send, data:")
			body, _ := json.Marshal(batchQueue)
			fmt.Println(string(body))
			return nil
		}
	}

	urlBatch := "https://m" + strings.ToLower(mon.Region) + ".getmetric.co/api1/batch"

	if mon.sendPort > 0 {
		// debug url
		urlBatch = "http://localhost:" + strconv.Itoa(mon.sendPort) + "/api1/batch"
	}

	body, err := json.Marshal(batchQueue)
	if err != nil {
		return err
	}

	if isDebugRun {
		fmt.Println(string(body))
	}

	resp, err := http.Post(urlBatch, "application/json", bytes.NewBufferString(string(body)))
	if err != nil {
		if isDebugRun {
			fmt.Println("Failed to send batch. error:" + err.Error())
		}
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != 200 {
		if isDebugRun {
			fmt.Println("Failed to send batch. Status code:", resp.StatusCode)
		}
		return fmt.Errorf("unknown status code %d", resp.StatusCode)
	}

	// TODO: get posted count from answer

	return nil
}

func (mon *Monitoring) GetSentCount() int {
	return mon.sentCount
}

func (mon *Monitoring) pushPerSecMeasureToQueue(item monItemV1) error {
	mon.perSecQueueMutex.Lock()
	defer mon.perSecQueueMutex.Unlock()

	if existing, ok := mon.perSecMap[item.Code]; ok {
		if len(item.Values) != len(existing.Values) || item.Type != existing.Type {
			// overwrite with last
			mon.perSecMap[item.Code] = item
			return fmt.Errorf("different measures")
		}

		// check time
		diffSec := int(item.Time.Sub(existing.Time).Seconds())
		if diffSec > 0 {
			if isDebugRun {
				fmt.Println("diff sec", diffSec)
			}

			// send to queue
			err := mon.pushMeasureToQueue(existing)

			// set as new
			mon.perSecMap[item.Code] = item

			return err
		}

		// increment values (only int and float are supported)
		for idx := range existing.Values {
			for _, val := range item.Values {
				if existing.Values[idx].Name == val.Name {
					switch existing.Values[idx].Value.(type) {
					case int:
						num := existing.Values[idx].Value.(int)
						if add, ok := val.Value.(int); ok {
							existing.Values[idx].Value = num + add
						}
					case float64:
						num := existing.Values[idx].Value.(float64)
						if add, ok := val.Value.(float64); ok {
							existing.Values[idx].Value = num + add
						}
					}
				}
			}

			// set new values
			mon.perSecMap[item.Code] = existing
		}

	} else {
		// add new
		mon.perSecMap[item.Code] = item
	}

	return nil
}
