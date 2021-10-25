package getmetric

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	Type   int        `json:"type"` // 1 - measure, 2 - operation
	Code   string     `json:"code"`
	Time   time.Time  `json:"time"`
	Values []MonValue `json:"values"`
}

func NewMonitoring(sendPeriodSec int) (*Monitoring, error) {

	if sendPeriodSec < 1 {
		return nil, fmt.Errorf("send per second must be greater than 0")
	}

	mon := Monitoring{}
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

		mon.checkAndDumpPerSec()

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

	urlBatch := "https://node.getmetric.net/api1/batch"

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

	contentType := "application/json"
	var reader io.Reader
	if len(body) > 1024 * 64 {
		// TODO: use snappy to compress data
		// contentType = "application/octet-stream"
	} else {
		reader = bytes.NewBufferString(string(body))
	}

	resp, err := http.Post(urlBatch, contentType, reader)
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

func (mon *Monitoring) checkAndDumpPerSec() {
	mon.perSecQueueMutex.Lock()
	defer mon.perSecQueueMutex.Unlock()

	now := time.Now().UTC()

	var remove []string
	for k, existing := range mon.perSecMap {
		diffSec := int(now.Sub(existing.Time).Seconds())
		if diffSec > 0 {
			if isDebugRun {
				fmt.Println("checkAndDumpPerSec diff sec", diffSec)
			}
			// send to queue
			_ = mon.pushMeasureToQueue(existing)
			remove = append(remove, k)
		}
	}

	for _, k := range remove {
		delete(mon.perSecMap, k)
	}
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

func (mon *Monitoring) PushOperation(code string, group int, stage string, isError bool, isEnd bool,
	message string, data map[string]interface{}) error {

	if !mon.running {
		return fmt.Errorf("not running")
	}

	if len(code) < 1 {
		// just ignore (monitoring item is not set up)
		return nil
	}

	if len(message) > 0 {
		if data == nil {
			data = make(map[string]interface{}, 0)
		}
		data["_message"] = message
	}

	dataJson := ""
	if data != nil && len(data) > 0 {
		dataJsonBt, err := json.Marshal(data)
		if err == nil {
			dataJson = string(dataJsonBt)
		}
	}

	isErrorStr := "false"
	if isError {
		isErrorStr = "true"
	}

	isEndStr := "false"
	if isEnd {
		isEndStr = "true"
	}

	return mon.pushMeasureToQueue(monItemV1{
		Type: 2,
		Code: code,
		Time: time.Now().UTC(),
		Values: []MonValue{{
			Name:  "group",
			Value: group,
		}, {
			Name:  "stage",
			Value: stage,
		}, {
			Name:  "error",
			Value: isErrorStr,
		}, {
			Name:  "end",
			Value: isEndStr,
		}, {
			Name:  "message",
			Value: dataJson,
		}}})
}

