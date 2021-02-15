
# System

To implement the system layer functions into the _Device Management Agent_ we
make use of the _CGO_ support in _golang_.

#### References

- https://karthikkaranth.me/blog/calling-c-code-from-go/
- https://dave.cheney.net/tag/cgo
- http://dominik.honnef.co/posts/2015/06/statically_compiled_go_programs__always__even_with_cgo__using_musl/

## Network

### iw

On Ubuntu 20.10 you need:

```
apt-get install libnl-3-dev libnl-genl-3-dev
```

#### References

- https://wireless.wiki.kernel.org/en/users/documentation/iw
- https://git.kernel.org/pub/scm/linux/kernel/git/jberg/iw.git
- https://linux.die.net/man/8/iw

### NetworkManager

Prerequisites:
- libglib2.0-dev
- libnm-dev
- network-manager-dev

#### References

- https://gitlab.freedesktop.org/NetworkManager/NetworkManager
- https://cgit.freedesktop.org/NetworkManager/NetworkManager/tree/examples/C/glib/get-ap-info-libnm.c
- https://cgit.freedesktop.org/NetworkManager/NetworkManager/tree/examples/C/glib/get-ap-info-libnm.c
- https://cgit.freedesktop.org/NetworkManager/NetworkManager/tree/examples/C/glib/
- https://www.redhat.com/sysadmin/becoming-friends-networkmanager
- https://developer.gnome.org/libnm/stable/usage.html
- https://ubuntu.pkgs.org/20.04/ubuntu-main-armhf/libnm-dev_1.22.10-1ubuntu1_armhf.deb.html
- https://developer.gnome.org/gobject/stable/gobject-The-Base-Object-Type.html
