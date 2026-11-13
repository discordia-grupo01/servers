package main

import "testing"

func TestResolveVoiceQueue(t *testing.T) {
	t.Setenv("VOICE_EVENTS_QUEUE", "voice.signaling")
	t.Setenv("VOICE_INSTANCE_ID", "environment-instance")

	tests := []struct {
		name       string
		queue      string
		instanceID string
		want       string
	}{
		{
			name:  "explicit queue",
			queue: "custom.queue",
			want:  "custom.queue",
		},
		{
			name:       "explicit instance",
			instanceID: "cli-instance",
			want:       "voice.signaling.cli-instance.replica",
		},
		{
			name: "environment instance",
			want: "voice.signaling.environment-instance.replica",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVoiceQueue(tt.queue, tt.instanceID); got != tt.want {
				t.Fatalf("resolveVoiceQueue(%q, %q) = %q, want %q",
					tt.queue, tt.instanceID, got, tt.want)
			}
		})
	}
}
