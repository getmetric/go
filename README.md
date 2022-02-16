# Getmetric Go library

## Install

With a [correctly configured](https://golang.org/doc/install#testing) Go toolchain:

```sh
go get -u github.com/getmetric/go
```

## Examples

### Sending measurement
Example shows how to pass value 10 to Getmetric measurement:

```go
import "github.com/getmetric/go"
func main() {
   // create Getmetric object with send timeout equal to 10 seconds 
   mon, _ := getmetric.NewMonitoring(10)
   mon.PushMeasure("YOUR_MEASUREMENT_CODE_HERE", 10)
    
   // make sure measurement has been sent
   time.Sleep(time.Duration(11) * time.Second)
}
```

NOTE: Measurement code can be taken from https://getmetric.net/metrics -> select and edit measurement, click Usage/Examples tab, check API key for measurement

### Sending per/second measurement
Example shows how to measure operations per seconds using measurement value

```go
import "github.com/getmetric/go"
func main() {
   // create Getmetric object with send timeout equal to 10 seconds 
   mon, _ := getmetric.NewMonitoring(10)
    
   for x := 0; x < 100; x++ {
      mon.PushPerSecondMeasure("YOUR_MEASUREMENT_CODE_HERE", 1)
      time.Sleep(time.Duration(10) * time.Millisecond)
   }
    
   // make sure measurement has been sent
   time.Sleep(time.Duration(11) * time.Second)
}
```
This is very useful when you need to calculate how much iteration per second you have withing
specific process, for example queries per second, API calls per second, etc.

### Sending operation
Example show how to use operation metric

```go
import "github.com/getmetric/go"
func main() {
   // create Getmetric object with send timeout equal to 10 seconds 
   mon, _ := getmetric.NewMonitoring(10)
    
   // send operation start
   mon.PushOperation("YOUR_OPERATION_CODE_HERE", "begin", false, "start", nil)

   // There goes some time consuming operation 
   time.Sleep(time.Duration(15) * time.Seconds)

   // send operation end
   mon.PushOperation("YOUR_OPERATION_CODE_HERE", "end", false, "finish", nil)

   // make sure operation has been sent
   time.Sleep(time.Duration(11) * time.Second)
}
```

NOTE: Operation code can be taken from https://getmetric.net/metrics -> select and edit operation, click Usage/Examples tab, check API key for measurement


### TODO: Log
