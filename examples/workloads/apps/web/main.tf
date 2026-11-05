terraform {
  required_providers {
    wm = { source = "homelab/wm" }
  }
}

resource "wm_workload" "service" {
  host    = "controller-example"
  desired = "running"
  binding = abspath("${path.module}/bindings/service.json")

  runtime {
    driver = "docker"
    engine = "application"
    # Replace this synthetic reference with the captured image's RepoDigest.
    image = "example/web@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
  }

  requirements {
    api = "docker-health"
  }

  recovery {
    restart       = false
    startup_grace = "5m"
  }

  diagnostics {
    logs = false
  }
}

output "status" {
  value = {
    id      = wm_workload.service.id
    desired = wm_workload.service.desired
    ready   = wm_workload.service.ready
  }
}
