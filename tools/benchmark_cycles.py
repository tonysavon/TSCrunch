#!/usr/bin/env python3
"""TSCrunch port of KabutoCrunch's six-game NMOS 6502 cycle benchmark."""

from __future__ import annotations

import argparse
import collections
import csv
import hashlib
import importlib.metadata
import json
import pathlib
import re
import subprocess
import sys
import tempfile

from py65.devices.mpu6502 import MPU

ROOT = pathlib.Path(__file__).resolve().parents[1]
PARENT = ROOT.parent
ENTRY = 0x0200
STOP = 0x0213
STUB_SIZE = STOP - ENTRY + 1
MAX_INSTRUCTIONS = 20_000_000
KICKASS = pathlib.Path(r"C:\C64-Kickass-IDE\bin\KickAss.jar")
DEFAULT_CASES = (
    ("kabutocrunch", PARENT / "kabutocrunch/game.prg"),
    ("crackpots", PARENT / "crackpots_working_folder/src/game.prg"),
    ("abbey", PARENT / "abbey/src/abbey.prg"),
    ("fixit", PARENT / "fixitfelixjr/src/game.prg"),
    ("pigquest", PARENT / "pigquest/src/game.prg"),
    ("keystone", PARENT / "KeystoneKapers/src/game.prg"),
)
DECODERS = {"regular": "decrunch.asm", "small": "decrunch_small.asm",
            "extreme": "decrunch_extreme.asm"}


class BenchmarkError(RuntimeError):
    pass


def run(command, cwd=ROOT):
    result = subprocess.run(list(map(str, command)), cwd=cwd, text=True, capture_output=True)
    if result.returncode:
        raise BenchmarkError(f"command failed: {command}\n{result.stdout}{result.stderr}")
    return result.stdout


def build_encoder(directory, source=None):
    executable = directory / ("tscrunch-cycle.exe" if sys.platform == "win32" else "tscrunch-cycle")
    run(["gcc", "-O3", "-std=c99", "-Wall", "-Wextra", "-I", ROOT,
         source or ROOT / "tscrunch.c", "-o", executable, "-lm"])
    return executable


def assemble_decoder(jar, directory, decoder, address=ENTRY + STUB_SIZE):
    source = directory / "cycle_decoder.asm"
    output = directory / "cycle_decoder.prg"
    source.write_text(
        '* = $0200 "cycle benchmark entry"\n'
        'lda #$00\nsta.zp tsget\nlda #$00\nsta.zp tsget+1\n'
        'lda #$00\nsta.zp tsput\nlda #$00\nsta.zp tsput+1\n'
        'jsr tsdecrunch\nbrk\n'
        f'* = ${address:04x} "decoder"\n'
        f'#import "{pathlib.Path(decoder).resolve().as_posix()}"\n', encoding="ascii")
    run(["java", "-jar", jar, source, "-o", output, "-symbolfile"])
    image = output.read_bytes()
    if int.from_bytes(image[:2], "little") != ENTRY:
        raise BenchmarkError("unexpected assembled load address")
    symbols = {}
    for name, value in re.findall(r"\.label\s+(\S+)\s*=\s*\$([0-9a-fA-F]+)",
                                  source.with_suffix(".sym").read_text()):
        symbols[name] = int(value, 16)
    return image[2:], symbols


def install_undocumented(cpu, legacy=False):
    # Instance-local tables: py65 otherwise shares these with every MPU instance.
    for name in ("instruct", "cycletime", "extracycles", "disassemble"):
        setattr(cpu, name, list(getattr(cpu, name)))

    def alr(mpu):
        mpu.a &= mpu.ByteAt(mpu.pc)
        mpu.pc += 1
        mpu.p = (mpu.p & ~mpu.CARRY) | (mpu.a & 1)
        mpu.a >>= 1
        mpu.FlagsNZ(mpu.a)

    def anc(mpu):
        mpu.a &= mpu.ByteAt(mpu.pc)
        mpu.pc += 1
        mpu.p = (mpu.p & ~mpu.CARRY) | (1 if mpu.a & 128 else 0)
        mpu.FlagsNZ(mpu.a)

    def lax_zp(mpu):
        mpu.a = mpu.x = mpu.ByteAt(mpu.ByteAt(mpu.pc))
        mpu.pc += 1
        mpu.FlagsNZ(mpu.a)

    def lax_indirect_y(mpu):
        mpu.a = mpu.x = mpu.ByteAt(mpu.IndirectYAddr())
        mpu.pc += 1
        mpu.FlagsNZ(mpu.a)

    for opcode, handler, cycles, extra, mode in (
        (0x0B, anc, 2, 0, ("ANC", "imm")),
        (0x2B, anc, 2, 0, ("ANC", "imm")),
        (0x4B, alr, 2, 0, ("ALR", "imm")),
        (0xA7, lax_zp, 3, 0, ("LAX", "zpg")),
        (0xB3, lax_indirect_y, 5, 0 if legacy else 1, ("LAX", "iny")),
    ):
        cpu.instruct[opcode] = handler
        cpu.cycletime[opcode] = cycles
        cpu.extracycles[opcode] = extra
        cpu.disassemble[opcode] = mode


def parse_stream(stream):
    """Decode and census the standard, non-inplace format independently."""
    if not stream:
        raise BenchmarkError("empty stream")
    zero_length = stream[0] + 1
    pos = 1
    output = bytearray()
    tokens = []
    while pos < len(stream):
        start = pos
        command = stream[pos]
        pos += 1
        distance = 0
        if command == 0x20:
            if pos != len(stream):
                raise BenchmarkError("trailing bytes after terminator")
            return bytes(output), tokens, zero_length
        if command < 0x20:
            if command == 0:
                raise BenchmarkError("invalid zero-length literal")
            kind, length = "literal", command
            output += stream[pos:pos + length]
            pos += length
        elif command < 0x80:
            kind, length, distance = "lz2", 2, 127 - command
        elif command & 1:
            if command == 0x81:
                kind, length, value = "zero", zero_length, 0
            else:
                kind, length, value = "rle", ((command & 127) >> 1) + 1, stream[pos]
                pos += 1
            output += bytes([value]) * length
        elif command & 2:
            kind, length, distance = "short", ((command & 127) >> 2) + 1, stream[pos]
            pos += 1
        else:
            high = stream[pos + 1]
            kind = "long"
            length = ((command & 127) >> 2) * 2 + (high >> 7) + 1
            negative = stream[pos] | ((high | 128) << 8)
            distance = 65536 - negative
            pos += 2
        if kind in ("lz2", "short", "long"):
            if not 1 <= distance <= len(output):
                raise BenchmarkError(f"invalid match distance {distance}")
            for _ in range(length):
                output.append(output[-distance])
        if pos > len(stream):
            raise BenchmarkError("truncated token")
        tokens.append({"kind": kind, "length": length, "distance": distance,
                       "packed": pos - start, "stream_offset": start,
                       "output_offset": len(output) - length})
    raise BenchmarkError("missing terminator")


class CheckedMemory:
    def __init__(self, destination, expected):
        self.data = bytearray(65536)
        self.destination = destination
        self.expected = expected
        self.running = False
        self.cpu = None
        self.writes = bytearray(len(expected))

    def __len__(self):
        return 65536

    def __getitem__(self, address):
        return self.data[address]

    def __setitem__(self, address, value):
        if isinstance(address, slice):
            self.data[address] = value
            return
        address &= 65535
        if self.running and self.destination <= address < self.destination + len(self.expected):
            index = address - self.destination
            if value != self.expected[index]:
                raise BenchmarkError(f"wrong output +${index:04x} at PC ${self.cpu.pc:04x}: "
                                     f"got ${value:02x}, expected ${self.expected[index]:02x}")
            self.writes[index] = 1
        self.data[address] = value & 255


def count_cycles(payload, stream, expected, destination, symbols=None, legacy=False,
                 stop=STOP, entry_offsets=(1, 5, 9, 13), tokens=None):
    source = 0xFFF0 - len(stream)  # Exactly KabutoCrunch's placement policy.
    if source <= ENTRY + len(payload):
        raise BenchmarkError("packed stream overlaps decoder")
    if destination + len(expected) > 65536:
        raise BenchmarkError("decompressed output wraps past 64 KiB")
    if destination < ENTRY + len(payload) and destination + len(expected) > ENTRY:
        raise BenchmarkError("output overlaps decoder")
    memory = CheckedMemory(destination, expected)
    memory.data[ENTRY:ENTRY + len(payload)] = payload
    memory.data[source:source + len(stream)] = stream
    for offset, value in zip(entry_offsets, (source & 255, source >> 8,
                                            destination & 255, destination >> 8)):
        memory.data[ENTRY + offset] = value
    cpu = MPU(memory=memory, pc=ENTRY)
    install_undocumented(cpu, legacy)
    memory.cpu = cpu
    memory.running = True
    token_entry = (symbols or {}).get("entry2")
    token_index = -1
    token_start = 0
    instruction_count = 0
    while cpu.pc != stop:
        if tokens is not None and cpu.pc == token_entry:
            if token_index >= 0:
                tokens[token_index]["cycles"] = cpu.processorCycles - token_start
            token_index += 1
            token_start = cpu.processorCycles
            if token_index > len(tokens):
                raise BenchmarkError("unexpected extra token")
        if cpu.disassemble[memory[cpu.pc]][0] == "???":
            raise BenchmarkError(f"unsupported opcode at ${cpu.pc:04x}")
        cpu.step()
        instruction_count += 1
        if instruction_count > MAX_INSTRUCTIONS:
            raise BenchmarkError(f"decoder did not terminate at ${cpu.pc:04x}")
    memory.running = False
    if bytes(memory.data[destination:destination + len(expected)]) != expected:
        raise BenchmarkError("final decompressed data differs from input")
    if 0 in memory.writes:
        raise BenchmarkError("decoder failed to write every output byte")
    return cpu.processorCycles


def select_cases(paths):
    cases = tuple((path.stem, path.resolve()) for path in paths) if paths else DEFAULT_CASES
    missing = [str(path) for _, path in cases if not path.is_file()]
    if missing:
        raise BenchmarkError("missing corpus files (the default requires all six):\n" + "\n".join(missing))
    return cases


def census(tokens):
    result = {}
    for kind in ("literal", "lz2", "short", "long", "rle", "zero"):
        group = [t for t in tokens if t["kind"] == kind]
        result[kind] = {"tokens": len(group), "output": sum(t["length"] for t in group),
                        "packed": sum(t["packed"] for t in group),
                        "cycles": sum(t.get("cycles", 0) for t in group),
                        "lengths": dict(sorted(collections.Counter(t["length"] for t in group).items()))}
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("prg", nargs="*", type=pathlib.Path)
    parser.add_argument("--kickass-jar", type=pathlib.Path, default=KICKASS)
    parser.add_argument("--decoder", choices=[*DECODERS, "all"], default="all")
    parser.add_argument("--decoder-address", type=lambda s: int(s, 0), default=ENTRY + STUB_SIZE)
    parser.add_argument("--encoder", type=pathlib.Path, help="use an existing encoder executable")
    parser.add_argument("--json", type=pathlib.Path)
    parser.add_argument("--csv", type=pathlib.Path)
    parser.add_argument("--streams-dir", type=pathlib.Path)
    parser.add_argument("--legacy-lax-cycles", action="store_true",
                        help="reproduce Kabuto's original missing LAX (zp),Y page penalty")
    args = parser.parse_args()
    cases = select_cases(args.prg)
    rows, identities, profiles = [], [], {}
    with tempfile.TemporaryDirectory(prefix="tscrunch-cycles-") as temp_name:
        temp = pathlib.Path(temp_name)
        encoder = args.encoder.resolve() if args.encoder else build_encoder(temp)
        decoders = DECODERS if args.decoder == "all" else {args.decoder: DECODERS[args.decoder]}
        images = {name: assemble_decoder(args.kickass_jar, temp, ROOT / filename,
                                         args.decoder_address) for name, filename in decoders.items()}
        for name, (payload, _) in images.items():
            print(f"{name} decoder: {len(payload) - (args.decoder_address - ENTRY)} bytes")
        print("decoder game          packed  output      cycles  cycles/byte")
        for name, path in cases:
            prg = path.read_bytes()
            if len(prg) < 3:
                raise BenchmarkError(f"invalid PRG: {path}")
            destination, expected = int.from_bytes(prg[:2], "little"), prg[2:]
            raw, packed = temp / "input.bin", temp / "packed.bin"
            raw.write_bytes(expected)
            run([encoder, "-q", raw, packed])
            stream = packed.read_bytes()
            decoded, tokens, zero_length = parse_stream(stream)
            if decoded != expected:
                raise BenchmarkError("independent format decode differs from input")
            identities.append({"game": name, "path": str(path), "sha256": hashlib.sha256(prg).hexdigest(),
                               "load_address": destination, "output": len(expected),
                               "zero_length": zero_length, "stream_sha256": hashlib.sha256(stream).hexdigest()})
            if args.streams_dir:
                args.streams_dir.mkdir(parents=True, exist_ok=True)
                (args.streams_dir / f"{name}.tscrunch").write_bytes(stream)
            profiles[name] = {}
            for decoder, (payload, symbols) in images.items():
                token_profile = [dict(t) for t in tokens]
                cycles = count_cycles(payload, stream, expected, destination, symbols,
                                      args.legacy_lax_cycles, tokens=token_profile)
                profiles[name][decoder] = census(token_profile)
                row = {"decoder": decoder, "game": name, "packed": len(stream), "output": len(expected),
                       "cycles": cycles, "cycles_per_byte": cycles / len(expected),
                       "decoder_bytes": len(payload) - (args.decoder_address - ENTRY)}
                rows.append(row)
                print(f"{decoder:7s} {name:12s} {len(stream):6d} {len(expected):7d} {cycles:11d} {cycles / len(expected):12.6f}")
        for decoder in images:
            selected = [row for row in rows if row["decoder"] == decoder]
            totals = {key: sum(row[key] for row in selected) for key in ("packed", "output", "cycles")}
            print(f"{decoder:7s} {'TOTAL':12s} {totals['packed']:6d} {totals['output']:7d} "
                  f"{totals['cycles']:11d} {totals['cycles'] / totals['output']:12.6f}")
    if args.json:
        args.json.parent.mkdir(parents=True, exist_ok=True)
        args.json.write_text(json.dumps({"rows": rows, "corpus": identities, "profiles": profiles,
                                        "source_end": 0xFFF0, "decoder_address": args.decoder_address,
                                        "legacy_lax_cycles": args.legacy_lax_cycles,
                                        "py65_version": importlib.metadata.version("py65"),
                                        "encoder_source_sha256": hashlib.sha256((ROOT / "tscrunch.c").read_bytes()).hexdigest()
                                          if not args.encoder else None,
                                        "decoder_source_sha256": {name: hashlib.sha256((ROOT / filename).read_bytes()).hexdigest()
                                                                  for name, filename in decoders.items()}}, indent=2) + "\n")
    if args.csv:
        args.csv.parent.mkdir(parents=True, exist_ok=True)
        with args.csv.open("w", newline="") as file:
            writer = csv.DictWriter(file, fieldnames=list(rows[0]))
            writer.writeheader()
            writer.writerows(rows)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except (BenchmarkError, OSError) as error:
        raise SystemExit(f"error: {error}") from error
