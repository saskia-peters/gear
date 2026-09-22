# G.E.A.R. infra inputs (Story 7.6). All cost knobs + the image/registry are
# variables: the SAME layout provisions a small trial or a bigger instance, and
# switching clouds reuses these inputs with a new provider.

variable "project_id" {
  description = "GCP project id where the trial VM is provisioned"
  type        = string
}

variable "region" {
  description = "GCP region for the VM"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone for the VM (must be inside region)"
  type        = string
  default     = "us-central1-a"
}

variable "machine_type" {
  description = "GCE machine type — e2-micro is the minimal-cost trial default"
  type        = string
  default     = "e2-micro"
}

variable "spot" {
  description = "Use a preemptible (spot) instance for a cheaper, interruptible trial"
  type        = bool
  default     = false
}

variable "disk_size_gb" {
  description = "Boot disk size in GB (trial-friendly default)"
  type        = number
  default     = 20
}

variable "image_repo" {
  description = "Image reference the host pulls (registry-driven; no build on host)"
  type        = string
  default     = "gear-app:latest"
}

variable "app_origin" {
  description = "Public origin used for password-reset links (GEAR_APP_ORIGIN)"
  type        = string
  default     = "https://gear.example.org"
}

variable "app_port" {
  description = "Host port mapped to the app :8080"
  type        = number
  default     = 8080
}

variable "ingress_source_ranges" {
  description = "Source CIDRs allowed to reach the app port (edge/CDN in front later)"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "ssh_source_ranges" {
  description = "Source CIDRs allowed to SSH to the VM (operator-controlled; set to your office/IP — world-open SSH is a security risk)"
  type        = list(string)
  # Deliberately restrictive placeholder: TEST-NET-1 (RFC 5737) so an unedited
  # apply does NOT open SSH to the whole internet. The operator MUST set this.
  default = ["198.51.100.0/24"]
}

variable "repo_url" {
  description = "Public/cloneable repo URL the VM startup script uses to fetch the repo when GEAR_APP_DIR is empty (optional; if unset the operator copies the repo to the VM)"
  type        = string
  default     = ""
}

variable "static_ip" {
  description = "Reserve a static external IP (false = ephemeral, minimal cost)"
  type        = bool
  default     = false
}

variable "dns_name" {
  description = "DNS name for the A record when static_ip is true (optional)"
  type        = string
  default     = ""
}

variable "dns_managed_zone" {
  description = "GCP Cloud DNS managed zone name for the A record (must exist in the project; only used when dns_name is set)"
  type        = string
  default     = ""
}

variable "create_artifact_registry" {
  description = "Provision a GCP Artifact Registry repo for the push path (optional; the app image can live in any Docker v2 registry)"
  type        = bool
  default     = false
}

variable "artifact_registry_repo" {
  description = "Artifact Registry repo id (used when create_artifact_registry = true)"
  type        = string
  default     = "gear"
}

variable "enable_api_service" {
  description = "Enable the required GCP APIs (compute, artifactregistry). False if already enabled."
  type        = bool
  default     = true
}