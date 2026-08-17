package awsurl

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsoleURLs(t *testing.T) {
	assert.Contains(t, Console("instance", "i-1", "us-east-1"), "InstanceDetails")
	assert.Contains(t, Console("s3", "arn:aws:s3:::b", "us-east-1"), "s3/buckets/b")
	assert.Contains(t, Console("lambda", "arn:aws:lambda:us-east-1:1:function:fn", "us-east-1"), "functions/fn")
	assert.Equal(t, "https://aws.amazon.com/s3/pricing/", Pricing("s3", nil))
	assert.NotEmpty(t, Metrics("instance", "i-1", "us-east-1"))
	assert.Contains(t, Logs("lambda", "arn:aws:lambda:us-east-1:1:function:fn", "us-east-1"), "lambda")
}
