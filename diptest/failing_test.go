package diptest

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/zond/diplicity/game"
)

func TestFailure(t *testing.T) {
	t.Errorf("This test failed purposefully!!!!")
}
