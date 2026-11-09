package main

import "testing"

func TestParseContainerID(t *testing.T) {
	const id = "3f8b1c2a9d4e5f60718293a4b5c6d7e8f90123456789abcdef0123456789abcd"
	cases := []struct {
		name    string
		cgroup  string
		want    string
		wantHas bool
	}{
		{
			name:    "docker cgroup v1",
			cgroup:  "12:devices:/docker/" + id + "\n11:cpu:/docker/" + id,
			want:    id[:12],
			wantHas: true,
		},
		{
			name:    "docker cgroup v2 systemd",
			cgroup:  "0::/system.slice/docker-" + id + ".scope",
			want:    id[:12],
			wantHas: true,
		},
		{
			name:    "containerd systemd",
			cgroup:  "0::/system.slice/containerd.service/cri-containerd-" + id + ".scope",
			want:    id[:12],
			wantHas: true,
		},
		{
			name:    "k8s cri-o path",
			cgroup:  "0::/kubepods.slice/kubepods-burstable.slice/crio-" + id + ".scope",
			want:    id[:12],
			wantHas: true,
		},
		{
			name:    "宿主机进程（无容器）",
			cgroup:  "0::/init.scope",
			want:    "",
			wantHas: false,
		},
		{
			name:    "宿主机 user slice",
			cgroup:  "0::/user.slice/user-1000.slice/session-3.scope",
			want:    "",
			wantHas: false,
		},
	}

	for _, c := range cases {
		got := parseContainerID(c.cgroup)
		if c.wantHas && got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		if !c.wantHas && got != "" {
			t.Errorf("%s: got %q, want empty", c.name, got)
		}
	}
}

func TestNormalizeProcName(t *testing.T) {
	cases := map[string]string{
		"/usr/bin/python3.11":   "python3",
		"python3.10":            "python3",
		"vllm":                  "vllm",
		"/opt/conda/bin/python": "python",
		"":                      "",
		`"python3"`:             "python3",
	}
	for in, want := range cases {
		if got := normalizeProcName(in); got != want {
			t.Errorf("normalizeProcName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStripReplicaSuffix(t *testing.T) {
	cases := map[string]string{
		"vllm-1":       "vllm",
		"vllm-qwen3-2": "vllm-qwen3",
		"reranker":     "reranker",
		"api-8080":     "api",
	}
	for in, want := range cases {
		if got := stripReplicaSuffix(in); got != want {
			t.Errorf("stripReplicaSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}
