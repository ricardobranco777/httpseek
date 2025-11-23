[![GoDoc](https://godoc.org/github.com/ricardobranco777/httpseek?status.svg)](https://godoc.org/github.com/ricardobranco777/httpseek)
![Build Status](https://github.com/ricardobranco777/httpseek/actions/workflows/ci.yml/badge.svg)

Implement io.ReaderAt & io.ReadSeekCloser for HTTP files.

It doesn't use a cache.

Similar projects:
- https://github.com/jeffallen/seekinghttp (uses a read-ahead cache).
- https://github.com/DHowett/ranger (caches each block in a map).
