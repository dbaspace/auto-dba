package store

import "testing"

func TestUpdateUserProfileRequiresName(t *testing.T) {
	d := &DB{}
	if err := d.UpdateUserProfile("x", "  ", UserProfile{}); err == nil {
		t.Fatal("empty name should fail")
	}
}
