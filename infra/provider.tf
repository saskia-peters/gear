# GCP provider (Story 7.6). Everything is input-driven; the ONLY GCP-specific
# surface in the whole deployment. A future non-GCP target (IONOS etc.) adds a
# second provider + resource file here WITHOUT touching the app/compose/.env
# contract (the portability invariant).
provider "google" {
  project = var.project_id
  region  = var.region
}