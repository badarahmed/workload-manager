terraform {
  required_providers {
    wm = { source = "homelab/wm" }
  }
}

resource "wm_workload" "d3" {
  host    = "gpu-01"
  desired = "running"
  binding = abspath("${path.module}/bindings/d3.json")

  runtime {
    driver = "docker"
    engine = "vllm-sr"
    # Replace this synthetic reference with the captured image's RepoDigest.
    image = "example/engine@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
  }

  model {
    # Replace both placeholders with the actual served repository and commit.
    source   = "hf://example-org/example-model"
    revision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
  }

  requirements {
    api = "systemone"
  }

  recovery {
    restart       = true
    startup_grace = "10m"
  }

  diagnostics {
    logs = false
  }
}

output "status" {
  value = {
    id      = wm_workload.d3.id
    desired = wm_workload.d3.desired
    ready   = wm_workload.d3.ready
  }
}
