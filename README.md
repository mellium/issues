# issues

[![GoDoc](https://godoc.org/mellium.im/issues?status.svg)](https://godoc.org/mellium.im/issues)
[![License](https://img.shields.io/badge/license-FreeBSD-blue.svg)](https://opensource.org/licenses/BSD-2-Clause)

[![Buy Me A Coffee](https://www.buymeacoffee.com/assets/img/custom_images/purple_img.png)](https://www.buymeacoffee.com/samwhited)

The `issues` tool extracts a Bitbucket issue export and attempts to import it
into GitHub.

Right now it does not preserve issue IDs properly, is not idempotent, and does
not import comments; hopefully these shortcomings will be fixed soon.

To install and run, make sure `GOBIN` (`~/go/bin` by default) is in your `PATH`,
and then try:

```
$ go get mellium.im/issues
$ ./issues -help
```