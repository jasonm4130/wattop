# Release and Homebrew distribution

Publish wattop v0.1.0 and a public `jasonm4130/homebrew-wattop` tap.
Success means a downloaded release with verified checksums and provenance,
a working Homebrew installation, and documented updates.

Build arm64 archives with a macOS 14 deployment target. Hardware behavior on
older macOS releases remains subject to the documented private-framework limits.
Use a binary cask with an explicit macOS and architecture requirement.

The tap polls stable releases hourly and supports manual workflow dispatch.
It verifies the archive checksum and GitHub provenance, installs the candidate
on an Apple Silicon runner, then commits the tested cask using its own token.
No cross-repository token is needed. Scheduled workflows may be delayed by
GitHub; maintainers can run the updater after a release for immediate delivery.

Preserve protected main in wattop. Merge release changes only after required
checks pass. Test the first release and Homebrew installation on the real Mac.
