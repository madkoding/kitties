package kitty

import "testing"

func TestContainerName(t *testing.T) {
	if ContainerName("mimi") != "kitty-mimi" {
		t.Error(ContainerName("mimi"))
	}
	k := Kitty{Name: "x", Status: StatusCreated}
	if k.Status != "created" || StatusRunning != "running" || StatusStopped != "stopped" || StatusError != "error" {
		t.Error("statuses")
	}
}
