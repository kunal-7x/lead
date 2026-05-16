provider "digitalocean" {
  token             = var.do_token
  spaces_access_id  = var.do_spaces_key
  spaces_secret_key = var.do_spaces_secret
}

provider "b2" {
  application_key_id = var.b2_key
  application_key    = var.b2_secret
}
