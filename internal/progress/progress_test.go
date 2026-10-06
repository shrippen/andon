package progress

import "testing"

func TestSetListFinish(t *testing.T) {
	Set("b", "task.x", nil, 1, 4)
	Set("a", "task.x", nil, 0, 0)
	Set("b", "task.x", nil, 3, 4)

	list := List()
	if len(list) != 2 || list[0].Key != "a" || list[1].Done != 3 {
		t.Fatalf("list = %+v", list)
	}
	if list[1].Percent() != 75 || list[0].Percent() != 0 {
		t.Fatalf("percent = %d, %d", list[1].Percent(), list[0].Percent())
	}

	Finish("b")
	Finish("a")
	if _, ok := Get("b"); ok || len(List()) != 0 {
		t.Fatal("finished tasks still listed")
	}
}
