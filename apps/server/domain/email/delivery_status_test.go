package email

import "testing"

// TestDeliveryStatusForEvent pins the event → delivery-status mapping used by the
// delivery store, including the opened/clicked tracking events and the
// severity-split failure mapping.
func TestDeliveryStatusForEvent(t *testing.T) {
	tests := []struct {
		name     string
		event    string
		severity string
		want     EmailDeliveryStatus
		wantOK   bool
	}{
		{"delivered maps to delivered", "delivered", "", DeliveryStatusDelivered, true},
		{"opened maps to opened", "opened", "", DeliveryStatusOpened, true},
		{"clicked maps to clicked", "clicked", "", DeliveryStatusClicked, true},
		{"failed permanent maps to bounced", "failed", "permanent", DeliveryStatusBounced, true},
		{"failed temporary maps to soft_bounced", "failed", "temporary", DeliveryStatusSoftBounced, true},
		{"bounced maps to bounced", "bounced", "", DeliveryStatusBounced, true},
		{"rejected maps to failed", "rejected", "", DeliveryStatusFailed, true},
		{"dropped maps to failed", "dropped", "", DeliveryStatusFailed, true},
		{"complained maps to complained", "complained", "", DeliveryStatusComplained, true},
		{"unsubscribed maps to unsubscribed", "unsubscribed", "", DeliveryStatusUnsubscribed, true},
		{"unmapped event is ignored", "some-unknown-event", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deliveryStatusForEvent(tt.event, tt.severity)
			if ok != tt.wantOK {
				t.Fatalf("deliveryStatusForEvent(%q, %q) ok = %v, want %v", tt.event, tt.severity, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("deliveryStatusForEvent(%q, %q) = %q, want %q", tt.event, tt.severity, got, tt.want)
			}
		})
	}
}
