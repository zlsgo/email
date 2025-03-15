package email

import (
	"testing"

	"github.com/sohaha/zlsgo"
)

func Test_formatEmailAddress(t *testing.T) {
	tt := zlsgo.NewTest(t)
	tt.Equal("<test@example.com>", formatEmailAddress("test@example.com"))
}
