load("//tools:defs.bzl", "go_library", "go_test")

package(
    default_applicable_licenses = ["//:license"],
    licenses = ["notice"],
)

go_library(
    name = "dxgproxy",
    srcs = [
        "buffers.go",
        "dxgproxy.go",
        "ipc.go",
        "mappings.go",
    ],
    visibility = [
        "//pkg/sentry:internal",
        "//runsc:__subpackages__",
    ],
    deps = [
        "//pkg/abi/dxg",
        "//pkg/abi/linux",
        "//pkg/context",
        "//pkg/devutil",
        "//pkg/errors/linuxerr",
        "//pkg/hostarch",
        "//pkg/log",
        "//pkg/safemem",
        "//pkg/sentry/arch",
        "//pkg/sentry/kernel",
        "//pkg/sentry/kernel/auth",
        "//pkg/sentry/mm",
        "//pkg/sentry/platform",
        "//pkg/sentry/vfs",
        "//pkg/sync",
        "//pkg/usermem",
        "@org_golang_x_sys//unix:go_default_library",
    ],
)

go_test(
    name = "dxgproxy_test",
    size = "small",
    srcs = ["mappings_test.go"],
    library = ":dxgproxy",
    deps = [
        "//pkg/abi/dxg",
        "//pkg/hostarch",
    ],
)
