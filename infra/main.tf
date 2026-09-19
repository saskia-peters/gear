# G.E.A.R. GCP resources (Story 7.6): a single Compute Engine VM running the
# compose stack (app + postgres containers). NO Cloud Run / Cloud SQL / Secret
# Manager — the container + a 0600 .env on the host are the portable units.
# The VM startup script is deploy/startup.sh (idempotent: install docker,
# generate secrets, compose pull + up, healthz wait).

# --- APIs (optional: skip if already enabled in the project) -----------------
resource "google_project_service" "compute" {
  count   = var.enable_api_service ? 1 : 0
  project = var.project_id
  service = "compute.googleapis.com"
}

resource "google_project_service" "artifactregistry" {
  count   = var.create_artifact_registry ? 1 : 0
  project = var.project_id
  service = "artifactregistry.googleapis.com"
}

# --- Optional static IP + DNS ------------------------------------------------
resource "google_compute_address" "app" {
  count   = var.static_ip ? 1 : 0
  name    = "gear-app"
  project = var.project_id
  region  = var.region
}

# --- Optional Artifact Registry repo (push path) ------------------------------
resource "google_artifact_registry_repository" "app" {
  count        = var.create_artifact_registry ? 1 : 0
  location     = var.region
  project      = var.project_id
  repository_id = var.artifact_registry_repo
  format       = "DOCKER"
}

# --- Service account with ONLY the registry-reader role ----------------------
# Minimal IAM: the VM needs to pull the image; nothing else (no Secret Manager,
# no Cloud SQL — there is none). The SA is created but not used when the image
# is in a public/external registry (image_repo not pointing at this project).
resource "google_service_account" "gear" {
  account_id   = "gear-app"
  display_name = "G.E.A.R. app VM (registry pull only)"
  project      = var.project_id
}

resource "google_project_iam_member" "artifact_reader" {
  count   = var.create_artifact_registry ? 1 : 0
  project = var.project_id
  role    = "roles/artifactregistry.reader"
  member  = "serviceAccount:${google_service_account.gear.email}"
}

# --- Firewall -----------------------------------------------------------------
resource "google_compute_firewall" "app" {
  name    = "gear-app-ingress"
  project = var.project_id
  network = "default"

  # Allow the app port from the ingress ranges (the edge/CDN layer terminates
  # TLS in front later — this rule is the app reachability, not TLS itself).
  allow {
    protocol = "tcp"
    ports    = [var.app_port]
  }
  source_ranges = var.ingress_source_ranges
}

resource "google_compute_firewall" "ssh" {
  name    = "gear-ssh-ingress"
  project = var.project_id
  network = "default"

  allow {
    protocol = "tcp"
    ports    = ["22"]
  }
  source_ranges = var.ssh_source_ranges
}

# --- The VM -------------------------------------------------------------------
data "google_compute_image" "debian" {
  family  = "debian-12"
  project = "debian-cloud"
}

resource "google_compute_instance" "app" {
  name         = "gear-app"
  project      = var.project_id
  zone         = var.zone
  machine_type = var.machine_type

  # Spot (preemptible) for a cheaper trial when enabled.
  scheduling {
    preemptible       = var.spot
    automatic_restart = !var.spot
  }

  boot_disk {
    initialize_params {
      image = data.google_compute_image.debian.self_link
      size  = var.disk_size_gb
    }
  }

  network_interface {
    network = "default"
    access_config {
      nat_ip = var.static_ip ? google_compute_address.app[0].address : null
    }
  }

  service_account {
    email  = google_service_account.gear.email
    scopes = ["cloud-platform"] # pull access to Artifact Registry
  }

  metadata_startup_script = templatefile("${path.module}/startup.tftpl", {
    app_origin = var.app_origin
    image_repo = var.image_repo
    app_port   = var.app_port
    repo_url   = var.repo_url
  })

  # /healthz via the app port (firewall already allows it). Wait for the
  # compute API to be enabled so a first apply does not race enablement.
  depends_on = [
    google_compute_firewall.app,
    google_compute_firewall.ssh,
    google_project_service.compute,
  ]
}

# --- Optional DNS A record ------------------------------------------------------
resource "google_dns_record_set" "app" {
  count        = var.static_ip && var.dns_name != "" && var.dns_managed_zone != "" ? 1 : 0
  name         = "${var.dns_name}."
  type         = "A"
  ttl          = 300
  managed_zone = var.dns_managed_zone
  project      = var.project_id
  rrdatas      = [google_compute_address.app[0].address]
}

# --- Outputs -------------------------------------------------------------------
output "instance_ip" {
  description = "External IP of the G.E.A.R. VM (ephemeral unless static_ip=true)"
  value       = var.static_ip ? google_compute_address.app[0].address : google_compute_instance.app.network_interface[0].access_config[0].nat_ip
}

output "app_url" {
  description = "App origin (the edge/CDN layer will terminate TLS in front later)"
  value       = "http://${var.static_ip ? google_compute_address.app[0].address : google_compute_instance.app.network_interface[0].access_config[0].nat_ip}:${var.app_port}"
}