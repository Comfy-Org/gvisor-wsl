load("//tools:defs.bzl", "go_library", "go_test")

package(
    default_applicable_licenses = ["//:license"],
    licenses = ["notice"],
)

go_library(
    name = "dxg",
    srcs = ["ioctl.go"],
    visibility = ["//:sandbox"],
)

go_test(
    name = "dxg_test",
    size = "small",
    srcs = ["ioctl_test.go"],
    library = ":dxg",
)
