variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
  sensitive   = true
  default     = ""
}

variable "do_spaces_key" {
  type      = string
  sensitive = true
  default   = ""
}

variable "do_spaces_secret" {
  type      = string
  sensitive = true
  default   = ""
}

variable "b2_key" {
  type      = string
  sensitive = true
  default   = ""
}

variable "b2_secret" {
  type      = string
  sensitive = true
  default   = ""
}

variable "region" {
  type        = string
  default     = "blr1"
  description = "DO region — Bangalore"
}

variable "project_name" {
  type    = string
  default = "lead"
}
