package backlog

import "testing"

func TestTransitionMatrix(t *testing.T) {
	tests := []struct {
		name              string
		from              Status
		to                Status
		actor             Actor
		agent, user, work string
		ok                bool
	}{
		{"triager starts", New, InProgress, Actor{Role: "triager"}, "", "", "", true},
		{"reporter cannot start", New, InProgress, Actor{Role: "reporter"}, "", "", "", false},
		{"agent submits", InProgress, Review, Actor{Kind: "agent", Role: "agent"}, "implemented", "", "verified", true},
		{"agent needs work log", InProgress, Review, Actor{Kind: "agent", Role: "agent"}, "implemented", "", "", false},
		{"admin reworks with comment", Review, Rework, Actor{Role: "admin"}, "", "fix edge case", "", true},
		{"admin rework requires comment", Review, Rework, Actor{Role: "admin"}, "", "", "", false},
		{"admin accepts", Review, Done, Actor{Role: "admin"}, "", "", "", true},
		{"agent cannot accept", Review, Done, Actor{Kind: "agent", Role: "agent"}, "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := allow(tt.from, TransitionInput{To: tt.to, AgentComment: tt.agent, UserComment: tt.user, WorkLog: tt.work}, tt.actor)
			if (err == nil) != tt.ok {
				t.Fatalf("allow=%v error=%v", tt.ok, err)
			}
		})
	}
}
func TestParseVersion(t *testing.T) {
	if v, err := ParseVersion(`"12"`); err != nil || v != 12 {
		t.Fatalf("version=%d err=%v", v, err)
	}
	if _, err := ParseVersion("12"); err == nil {
		t.Fatal("unquoted version accepted")
	}
}
