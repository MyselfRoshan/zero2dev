package supervisor

import (
	"strings"
	"testing"

	"zero2dev/internal/model"
)

func TestMailpitPorts(t *testing.T) {
	free := func(int) bool { return true }
	taken := func(p int) bool { return p != 1025 }
	mp := model.Service{Kind: "mailpit", Port: 8025}
	if smtpPort(mp) != 1025 || checkExtraPorts(mp, free) != nil {
		t.Fatal("the default SMTP port is 1025 and free")
	}
	if err := checkExtraPorts(mp, taken); err == nil || !strings.Contains(err.Error(), "SMTP port 1025 is already in use") {
		t.Fatalf("taken SMTP port: %v", err)
	}
	same := model.Service{Kind: "mailpit", Port: 2525, Settings: map[string]string{"smtpPort": "2525"}}
	if err := checkExtraPorts(same, free); err == nil || !strings.Contains(err.Error(), "different ports") {
		t.Fatalf("same ports: %v", err)
	}
	// Own arguments: Zero2Dev does not know the SMTP port. Other kinds have none.
	for _, sv := range []model.Service{{Kind: "mailpit", Port: 8025, CustomArgs: true}, {Kind: "node", Port: 1025}} {
		if smtpPort(sv) != 0 || checkExtraPorts(sv, taken) != nil {
			t.Errorf("%+v: no SMTP port check", sv)
		}
	}
	if healthPath(mp) != "/livez" || healthPath(model.Service{Kind: "mailpit", HealthPath: "/readyz"}) != "/readyz" || healthPath(model.Service{Kind: "node"}) != "" {
		t.Error("healthPath")
	}
}
