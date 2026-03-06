package discovery

import (
	"testing"
)

func TestParseFilename(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    ContainerRef
		wantErr bool
	}{
		{
			name:  "standard pod name",
			input: "/var/log/containers/api-gateway-7b8f9c6d4e-xk2p5_production_api-gateway-abc123def456.log",
			want: ContainerRef{
				Pod:       "api-gateway-7b8f9c6d4e-xk2p5",
				Namespace: "production",
				Container: "api-gateway",
				ID:        "abc123def456",
			},
		},
		{
			name:  "simple names",
			input: "nginx_default_nginx-abc123.log",
			want: ContainerRef{
				Pod:       "nginx",
				Namespace: "default",
				Container: "nginx",
				ID:        "abc123",
			},
		},
		{
			name:  "pod with many hyphens",
			input: "my-app-v2-backend-7b8f9c-xk2p5_kube-system_sidecar-proxy-deadbeef.log",
			want: ContainerRef{
				Pod:       "my-app-v2-backend-7b8f9c-xk2p5",
				Namespace: "kube-system",
				Container: "sidecar-proxy",
				ID:        "deadbeef",
			},
		},
		{
			name:  "long container ID",
			input: "pod_ns_ctr-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2.log",
			want: ContainerRef{
				Pod:       "pod",
				Namespace: "ns",
				Container: "ctr",
				ID:        "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			},
		},
		{
			name:    "not a log file",
			input:   "something.txt",
			wantErr: true,
		},
		{
			name:    "no underscore",
			input:   "nounderscore-abc123.log",
			wantErr: true,
		},
		{
			name:    "only one underscore",
			input:   "pod_container-abc123.log",
			wantErr: true,
		},
		{
			name:    "no container ID separator",
			input:   "pod_ns_containeronly.log",
			wantErr: true,
		},
		{
			name:    "empty fields",
			input:   "_ns_ctr-id.log",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFilename(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
