#!/usr/bin/env python3
"""Build C/Go/Java, compare their output and optionally verify the plugin API."""
from __future__ import annotations

import argparse
import json
import pathlib
import random
import subprocess
import tempfile

import benchmark_cycles as bench


PLUGIN_BRIDGE = '''import java.nio.file.*;
import java.util.Arrays;
public class VerifyPlugin {
    public static void main(String[] args) throws Exception {
        byte[] prg = Files.readAllBytes(Paths.get(args[0]));
        int address = (prg[0] & 255) | ((prg[1] & 255) << 8);
        byte[] result = tscrunch.TSCrunch.crunchForPlugin(
            Arrays.copyOfRange(prg, 2, prg.length), address,
            args[2].equals("inplace"), args[2].equals("sfx"), address);
        if (result == null) throw new IllegalStateException("plugin returned null");
        if (args[2].equals("inplace") || args[2].equals("sfx")) {
            int load = args[2].equals("sfx") ? 0x0801 : (address + prg.length - 2 - result.length) & 65535;
            byte[] withHeader = new byte[result.length + 2];
            withHeader[0] = (byte)load; withHeader[1] = (byte)(load >> 8);
            System.arraycopy(result, 0, withHeader, 2, result.length);
            result = withHeader;
        }
        Files.write(Paths.get(args[1]), result);
    }
}
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--plugin-root", type=pathlib.Path)
    parser.add_argument("--baseline-ref", default="4511a75",
                        help="encoder revision before cycle-aware parsing")
    parser.add_argument("--json", type=pathlib.Path)
    args = parser.parse_args()
    records = []
    with tempfile.TemporaryDirectory(prefix="tscrunch-parity-") as temp_name:
        temp = pathlib.Path(temp_name)
        baseline_source = temp / "baseline.c"
        baseline_source.write_bytes(subprocess.check_output(
            ["git", "show", f"{args.baseline_ref}:tscrunch.c"], cwd=bench.ROOT))
        baseline_dir = temp / "baseline"
        baseline_dir.mkdir()
        baseline = bench.build_encoder(baseline_dir, baseline_source)
        current_dir = temp / "current"
        current_dir.mkdir()
        current = bench.build_encoder(current_dir)
        go = temp / "tscrunch-go.exe"
        bench.run(["go", "build", "-o", go, "tscrunch.go"])
        classes = temp / "classes"
        classes.mkdir()
        bench.run(["javac", "--release", "11", "-d", classes, bench.ROOT / "TSCrunch.java"])
        commands = {"c": [current], "go": [go], "java": ["java", "-cp", classes, "TSCrunch"]}
        if args.plugin_root:
            plugin_classes = temp / "plugin-classes"
            plugin_classes.mkdir()
            bridge = temp / "VerifyPlugin.java"
            bridge.write_text(PLUGIN_BRIDGE)
            bench.run(["javac", "--release", "11", "-d", plugin_classes,
                       args.plugin_root / "src/main/java/tscrunch/TSCrunch.java", bridge])
        cases = list(bench.select_cases([]))
        rng = random.Random(0x6502)
        synthetic = {
            "zeros": bytes(1024),
            "rle-boundaries": b"".join(bytes([i])*n for i, n in enumerate((2, 3, 31, 32, 63, 64, 65, 255, 256))),
            "literal-pages": bytes(rng.randrange(256) for _ in range(1025)),
            "overlapping-matches": bytes([17, 34, 51])*300 + bytes(range(256))*3,
        }
        for name, raw in synthetic.items():
            path = temp / f"{name}.prg"
            path.write_bytes(bytes([0x01, 0x20]) + raw)
            cases.append((name, path))
        modes = ("raw", "inplace", "sfx", "sfx-stack", "sfx-blank")
        for name, path in cases:
            prg = path.read_bytes()
            address = int.from_bytes(prg[:2], "little")
            for mode in modes:
                flags = {"raw": ["-p"], "inplace": ["-i"], "sfx": ["-x", f"0x{address:04x}"],
                         "sfx-stack": ["-x2", f"0x{address:04x}"],
                         "sfx-blank": ["-b", "-x", f"0x{address:04x}"]}[mode]
                outputs = {}
                for implementation, command in commands.items():
                    output = temp / f"{implementation}.bin"
                    bench.run([*command, "-q", *flags, path, output])
                    outputs[implementation] = output.read_bytes()
                old = temp / "old.bin"
                bench.run([baseline, "-q", *flags, path, old])
                if any(data != outputs["c"] for data in outputs.values()):
                    raise bench.BenchmarkError(f"encoder output mismatch: {name}/{mode}")
                if len(outputs["c"]) > len(old.read_bytes()):
                    raise bench.BenchmarkError(f"packed size regression: {name}/{mode}")
                if mode == "raw" and bench.parse_stream(outputs["c"])[0] != prg[2:]:
                    raise bench.BenchmarkError(f"raw decode mismatch: {name}")
                if args.plugin_root and mode in ("raw", "inplace", "sfx"):
                    output = temp / "plugin.bin"
                    bench.run(["java", "-cp", plugin_classes, "VerifyPlugin", path, output, mode])
                    if output.read_bytes() != outputs["c"]:
                        raise bench.BenchmarkError(f"plugin API mismatch: {name}/{mode}")
                records.append({"case": name, "mode": mode, "packed": len(outputs["c"]),
                                "baseline_packed": len(old.read_bytes())})
            print(f"{name}: five modes byte-identical across C/Go/Java; no size regressions", flush=True)
    if args.json:
        args.json.parent.mkdir(parents=True, exist_ok=True)
        args.json.write_text(json.dumps(records, indent=2) + "\n")
    print(f"verified {len(records)} case/mode combinations" +
          (" and 30 plugin API comparisons" if args.plugin_root else ""))


if __name__ == "__main__":
    main()
