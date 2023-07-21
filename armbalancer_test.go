package armbalancer

import (
	"testing"
)

func TestNew(t *testing.T) {
	type args struct {
		opts Options
	}
	tests := []struct {
		name     string
		args     args
		wantHost string
		wantPort string
		paniced  bool
	}{
		{
			name: "invalid host",
			args: args{
				opts: Options{
					Host: "invalid:host:invalidport",
				},
			},
			paniced: true,
		},
		{
			name: "host is not assigned",
			args: args{
				opts: Options{
					Host: ":445",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "445",
			paniced:  false,
		},
		{
			name: "port is not assigned",
			args: args{
				opts: Options{
					Host: "management.azure.com",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "443",
			paniced:  false,
		},
		{
			name: "hosturl is not assigned",
			args: args{
				opts: Options{
					Host: "",
				},
			},
			wantHost: "management.azure.com",
			wantPort: "443",
			paniced:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.paniced {
				defer func() {
					if r := recover(); r != nil {
						return
					}
					t.Errorf("New() did not panic")
				}()
			}
			if got := New(tt.args.opts); got == nil {
				t.Errorf("New() returned nil")
			}
		})
	}
}
