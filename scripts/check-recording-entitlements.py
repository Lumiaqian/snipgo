"""Verify the signed microphone scope in every host and recorder slice."""
import plistlib
import subprocess
import sys
from pathlib import Path

contents = Path(sys.argv[1]) / "Contents"
host = contents / "MacOS/SnipGo"
helper = contents / "Resources/bin/SnipGoVideo"
mic = "com.apple.security.device.audio-input"
for binary in (host, helper):
    for arch in subprocess.check_output(["lipo", "-archs", binary], text=True).split():
        data = subprocess.check_output(
            ["codesign", "-d", "--arch", arch, "--xml", "--entitlements", "-", binary]
        )
        grants = plistlib.loads(data) if data else {}
        valid_mic = grants.get(mic) is True if binary == host else mic not in grants
        if not valid_mic or "com.apple.security.cs.disable-library-validation" in grants:
            sys.exit(f"Unexpected signed entitlements for {binary} ({arch}): {grants}")
        print(f"{binary} ({arch}): {grants}")
