# Every image the integration tests run, built in one parallel bake:
#
#   WP_VERSION=7.1 docker buildx bake --load
#
# tags presso-{edge,mariadb,valkey,browser,runtime}:release-verification.
# image-verification.yml sets PHP_VERSION, PHP_PATCH and WP_VERSION from
# `pressod runtime-matrix`. The published images are not built from this
# file: release.yml and runtime.yml build each image per architecture.

variable "TAG" {
  default = "release-verification"
}

variable "PHP_VERSION" {
  default = "8.5"
}

variable "PHP_PATCH" {
  default = "8.5.12"
}

variable "WP_VERSION" {
  default = ""
}

group "default" {
  targets = ["platform", "runtime"]
}

target "platform" {
  name       = image
  matrix     = { image = ["edge", "mariadb", "valkey", "browser"] }
  context    = "build"
  dockerfile = "${image}.Dockerfile"
  tags       = ["presso-${image}:${TAG}"]
}

target "runtime-base" {
  context    = "runtime"
  dockerfile = "base.Dockerfile"
  args = {
    PHP_VERSION = PHP_VERSION
    PHP_PATCH   = PHP_PATCH
  }
  tags = ["presso-runtime-base:${TAG}"]
}

target "runtime" {
  context    = "runtime"
  dockerfile = "Dockerfile"
  contexts = {
    base = "target:runtime-base"
  }
  args = {
    BASE_IMAGE = "base"
    WP_VERSION = WP_VERSION
  }
  tags = ["presso-runtime:${TAG}"]
}
