// Copyright 2026 The gVisor Authors.
// SPDX-License-Identifier: Apache-2.0
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/wait.h>
#include <unistd.h>

static int expect_ioctl(int fd, unsigned long cmd, void *arg, int expected) {
  errno = 0;
  int ret = ioctl(fd, cmd, arg);
  if (ret != -1 || errno != expected) {
    fprintf(stderr, "ioctl %#lx: ret=%d errno=%d, expected -1/%d\n", cmd, ret, errno, expected);
    return 1;
  }
  return 0;
}

int main(int argc, char **argv) {
  int fd = open("/dev/dxg", O_RDWR | O_CLOEXEC);
  if (argc == 2 && strcmp(argv[1], "denied") == 0) {
    if (fd >= 0) { fputs("DXG unexpectedly opened without authorization\n", stderr); close(fd); return 1; }
    if (errno != ENODEV && errno != ENXIO && errno != ENOENT && errno != EACCES && errno != EPERM) { perror("unexpected open error"); return 1; }
    puts("PASS: DXG access denied without opt-in/device authorization");
    return 0;
  }
  if (fd < 0) { perror("open dxg"); return 1; }
  int failed = 0;
  failed |= expect_ioctl(fd, 0xffffffff, NULL, ENOTTY);
  failed |= expect_ioctl(fd, 0xc018473e ^ (1u << 16), NULL, ENOTTY);
  failed |= expect_ioctl(fd, 0xc018473e, NULL, EFAULT);
  failed |= expect_ioctl(fd, 0xc018473e, (void *)(uintptr_t)-4096, EFAULT);
  uint64_t resource_info[5] = {0};
  resource_info[1] = 0xffffffffu;
  failed |= expect_ioctl(fd, 0xc0284741, resource_info, EBADF);
  resource_info[1] = (uint64_t)fd; // a device FD is NOT a shared-resource FD
  failed |= expect_ioctl(fd, 0xc0284741, resource_info, EINVAL);
  uint64_t share[5] = {0};
  failed |= expect_ioctl(fd, 0xc028473f, share, EINVAL); // exactly one object required
  pid_t pid = fork();
  if (pid < 0) { perror("fork"); return 1; }
  if (pid == 0) _exit(expect_ioctl(fd, 0xc018473e, NULL, EXDEV));
  int status;
  if (waitpid(pid, &status, 0) != pid || !WIFEXITED(status) || WEXITSTATUS(status) != 0) failed = 1;
  close(fd);
  if (!failed) puts("PASS: unsupported/malformed ioctls, invalid IPC resource FDs and cross-MM inherited FD rejected");
  return failed;
}
