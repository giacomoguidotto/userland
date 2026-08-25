#!/usr/bin/env python3

from __future__ import annotations

import re
import sys


data = open(sys.argv[1], "rb").read()
data = re.sub(rb"\r\x1b\[2K[^\r\n]*\xe2\x80\xa6 [^\r\n]*", b"", data)
data = data.replace(b"\r\x1b[2K", b"")
data = re.sub(rb" \((?:<1s|[0-9]+s)\)$", b" (<elapsed>)", data, flags=re.MULTILINE)
data = re.sub(rb"^    (?:<1s|[0-9]+s)$", b"    <elapsed>", data, flags=re.MULTILINE)
sys.stdout.buffer.write(data)
