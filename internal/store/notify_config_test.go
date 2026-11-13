package store

import "testing"

func TestDecodeNotifyConfig(t *testing.T) {
	empty, failed := decodeNotifyConfig("  ")
	if failed || empty == nil || empty.EmailOnTicketCreate || empty.LarkOnTicketCreate {
		t.Fatalf("empty=%+v failed=%v", empty, failed)
	}
	bad, failed := decodeNotifyConfig("{")
	if !failed || bad == nil || bad.EmailOnTicketCreate || bad.LarkOnTicketCreate {
		t.Fatalf("bad=%+v failed=%v", bad, failed)
	}
	ok, failed := decodeNotifyConfig(`{"email_on_ticket_create":true,"lark_on_ticket_create":true}`)
	if failed || ok == nil || !ok.EmailOnTicketCreate || !ok.LarkOnTicketCreate {
		t.Fatalf("ok=%+v failed=%v", ok, failed)
	}
}
