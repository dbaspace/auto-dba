package describelogdirs

import (
	"github.com/segmentio/kafka-go/protocol"
)

func init() {
	protocol.Register(&Request{}, &Response{})
}

// Request is Kafka DescribeLogDirs (API key 35).
// Spec: https://kafka.apache.org/protocol#The_Messages_DescribeLogDirs
type Request struct {
	Topics []RequestTopic `kafka:"min=v0,max=v1,nullable"`
}

type RequestTopic struct {
	Name       string  `kafka:"min=v0,max=v1"`
	Partitions []int32 `kafka:"min=v0,max=v1"`
}

func (r *Request) ApiKey() protocol.ApiKey { return protocol.DescribeLogDirs }

type Response struct {
	ThrottleTimeMs int32            `kafka:"min=v1,max=v1"`
	Results        []ResponseResult `kafka:"min=v0,max=v1"`
}

type ResponseResult struct {
	ErrorCode int16           `kafka:"min=v0,max=v1"`
	LogDir    string          `kafka:"min=v0,max=v1"`
	Topics    []ResponseTopic `kafka:"min=v0,max=v1"`
}

type ResponseTopic struct {
	Name       string              `kafka:"min=v0,max=v1"`
	Partitions []ResponsePartition `kafka:"min=v0,max=v1"`
}

type ResponsePartition struct {
	Partition int32 `kafka:"min=v0,max=v1"`
	Size      int64 `kafka:"min=v0,max=v1"`
	OffsetLag int64 `kafka:"min=v0,max=v1"`
	IsFuture  bool  `kafka:"min=v0,max=v1"`
}

func (r *Response) ApiKey() protocol.ApiKey { return protocol.DescribeLogDirs }
