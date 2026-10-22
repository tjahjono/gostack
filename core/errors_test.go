package core_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tjahjono/gostack/core"
)

func TestErrorsKeepSafeMessagesAndInternalCauses(t *testing.T) {
	cause := errors.New("private database detail")
	err := core.NewError(core.Unavailable, "storage unavailable", cause)
	if strings.Contains(err.Error(), "private") || !errors.Is(err, cause) || !core.IsCode(err, core.Unavailable) {
		t.Fatal("error boundary failed")
	}
}
func TestPaginationLimits(t *testing.T) {
	for _, limit := range []int{-1, 101} {
		if _, err := core.NewPageRequest(limit, ""); !core.IsCode(err, core.Invalid) {
			t.Fatal("unbounded pagination accepted")
		}
	}
	page, err := core.NewPageRequest(0, "")
	if err != nil || page.Limit() != 25 || (core.PageRequest{}).Limit() != 25 {
		t.Fatal("default pagination failed")
	}
}
