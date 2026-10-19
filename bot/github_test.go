package main

import (
	"errors"
	"strings"
	"testing"
)

func TestDeviceCodeProblem(t *testing.T) {
	for _, c := range []struct {
		err  error
		code string
		want string
	}{
		{errors.New("timeout"), "", "didn't answer"},
		{nil, "device_flow_disabled", "Device Flow is off"},
		{nil, "unauthorized_client", "client ID"},
		{nil, "", "client ID"},
		{nil, "weird", "(weird)"},
	} {
		if got := deviceCodeProblem(c.err, c.code); !strings.Contains(got, c.want) {
			t.Errorf("%v %q: %s", c.err, c.code, got)
		}
	}
}
