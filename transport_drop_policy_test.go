package armbalancer

import (
	"net/http"
	"testing"
)

func TestKillBeforeThrottledPolicy_ShouldDropTransport(t *testing.T) {
	type fields struct {
		RecycleThreshold int64
	}
	type args struct {
		header http.Header
	}
	tests := []struct {
		name   string
		fields fields
		args   args
		want   bool
	}{
		{
			name: "should drop",
			fields: fields{
				RecycleThreshold: 10,
			},
			args: args{
				header: http.Header{
					"X-Ms-Ratelimit-Remaining-Subscription-Reads": []string{"9"},
				},
			},
			want: true,
		},
		{
			name: "should drop",
			fields: fields{
				RecycleThreshold: 10,
			},
			args: args{
				header: http.Header{
					"X-Ms-Ratelimit-Remaining-Subscription-Reads": []string{"11"},
					"X-Ms-Ratelimit-Remaining-VMSS-Reads":         []string{"9"},
				},
			},
			want: true,
		},
		{
			name: "should do nothing",
			fields: fields{
				RecycleThreshold: 10,
			},
			args: args{
				header: http.Header{
					"X-Ms-Ratelimit-Remaining-Subscription-Reads": []string{"11"},
					"Accept": []string{"application/json"},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := &KillBeforeThrottledPolicy{
				RecycleThreshold: tt.fields.RecycleThreshold,
			}
			if got := policy.ShouldDropTransport(tt.args.header); got != tt.want {
				t.Errorf("KillBeforeThrottledPolicy.ShouldDropTransport() = %v, want %v", got, tt.want)
			}
		})
	}
}
