import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.stream.IntStream;

public class TSCrunch {
    private static final int LONGESTRLE = 64;
    private static final int LONGESTLONGLZ = 64;
    private static final int LONGESTLZ = 32;
    private static final int LONGESTLITERAL = 31;
    private static final int MINRLE = 2;
    private static final int MINLZ = 3;
    private static final int MINLZOFFSET = 2;
    private static final int LZOFFSET = 256;
    private static final int LONGLZOFFSET = 32767;
    private static final int LZ2OFFSET = 94;
    private static final int LZ2SIZE = 2;

    private static final int RLEMASK = 0x81;
    private static final int LZMASK = 0x80;
    private static final int LITERALMASK = 0x00;
    private static final int LZ2MASK = 0x00;

    private static final int TERMINATOR = LONGESTLITERAL + 1;

    private static final byte[] BOOT = new byte[] {
        (byte)0x01, (byte)0x08, (byte)0x0B, (byte)0x08, (byte)0x0A, (byte)0x00, (byte)0x9E, (byte)0x32,
        (byte)0x30, (byte)0x36, (byte)0x31, (byte)0x00, (byte)0x00, (byte)0x00, (byte)0x78, (byte)0xA2,
        (byte)0xCC, (byte)0xBD, (byte)0x1A, (byte)0x08, (byte)0x95, (byte)0x00, (byte)0xCA, (byte)0xD0,
        (byte)0xF8, (byte)0x4C, (byte)0x02, (byte)0x00, (byte)0x34, (byte)0xBD, (byte)0x00, (byte)0x10,
        (byte)0x9D, (byte)0x00, (byte)0xFF, (byte)0xE8, (byte)0xD0, (byte)0xF7, (byte)0xC6, (byte)0x07,
        (byte)0xA9, (byte)0x06, (byte)0xC7, (byte)0x04, (byte)0x90, (byte)0xEF, (byte)0xA0, (byte)0x00,
        (byte)0xB3, (byte)0x24, (byte)0x30, (byte)0x29, (byte)0xC9, (byte)0x20, (byte)0xB0, (byte)0x47,
        (byte)0xE6, (byte)0x24, (byte)0xD0, (byte)0x02, (byte)0xE6, (byte)0x25, (byte)0xB9, (byte)0xFF,
        (byte)0xFF, (byte)0x99, (byte)0xFF, (byte)0xFF, (byte)0xC8, (byte)0xCA, (byte)0xD0, (byte)0xF6,
        (byte)0x98, (byte)0xAA, (byte)0xA0, (byte)0x00, (byte)0x65, (byte)0x27, (byte)0x85, (byte)0x27,
        (byte)0xB0, (byte)0x74, (byte)0x8A, (byte)0x65, (byte)0x24, (byte)0x85, (byte)0x24, (byte)0x90,
        (byte)0xD7, (byte)0xE6, (byte)0x25, (byte)0xB0, (byte)0xD3, (byte)0x4B, (byte)0x7F, (byte)0x90,
        (byte)0x39, (byte)0xF0, (byte)0x68, (byte)0xA2, (byte)0x02, (byte)0x85, (byte)0x59, (byte)0xC8,
        (byte)0xB1, (byte)0x24, (byte)0xA4, (byte)0x59, (byte)0x91, (byte)0x27, (byte)0x88, (byte)0x91,
        (byte)0x27, (byte)0xD0, (byte)0xFB, (byte)0xA9, (byte)0x00, (byte)0xB0, (byte)0xD5, (byte)0xA9,
        (byte)0x37, (byte)0x85, (byte)0x01, (byte)0x58, (byte)0x4C, (byte)0x61, (byte)0x00, (byte)0xF0,
        (byte)0xF6, (byte)0x09, (byte)0x80, (byte)0x65, (byte)0x27, (byte)0x85, (byte)0xA0, (byte)0xA5,
        (byte)0x28, (byte)0xE9, (byte)0x00, (byte)0x85, (byte)0xA1, (byte)0xB1, (byte)0xA0, (byte)0x91,
        (byte)0x27, (byte)0xC8, (byte)0xB1, (byte)0xA0, (byte)0x91, (byte)0x27, (byte)0x98, (byte)0xAA,
        (byte)0xD0, (byte)0xB0, (byte)0x4A, (byte)0x85, (byte)0xA5, (byte)0xC8, (byte)0xA5, (byte)0x27,
        (byte)0x90, (byte)0x31, (byte)0xF1, (byte)0x24, (byte)0x85, (byte)0xA0, (byte)0xA5, (byte)0x28,
        (byte)0xE9, (byte)0x00, (byte)0x85, (byte)0xA1, (byte)0xA2, (byte)0x02, (byte)0xA0, (byte)0x00,
        (byte)0xB1, (byte)0xA0, (byte)0x91, (byte)0x27, (byte)0xC8, (byte)0xB1, (byte)0xA0, (byte)0x91,
        (byte)0x27, (byte)0xC8, (byte)0xB9, (byte)0xA0, (byte)0x00, (byte)0x91, (byte)0x27, (byte)0xC0,
        (byte)0x00, (byte)0xD0, (byte)0xF6, (byte)0x98, (byte)0xB0, (byte)0x84, (byte)0xE6, (byte)0x28,
        (byte)0x18, (byte)0x90, (byte)0x87, (byte)0xA0, (byte)0xFF, (byte)0x84, (byte)0x59, (byte)0xA2,
        (byte)0x01, (byte)0xD0, (byte)0x99, (byte)0x71, (byte)0x24, (byte)0x85, (byte)0xA0, (byte)0xC8,
        (byte)0xB3, (byte)0x24, (byte)0x09, (byte)0x80, (byte)0x65, (byte)0x28, (byte)0x85, (byte)0xA1,
        (byte)0xE0, (byte)0x80, (byte)0x26, (byte)0xA5, (byte)0xA2, (byte)0x03, (byte)0xD0, (byte)0xC6
    };

    private static final byte[] BLANK_BOOT = new byte[] {
        (byte)0x01, (byte)0x08, (byte)0x0B, (byte)0x08, (byte)0x0A, (byte)0x00, (byte)0x9E, (byte)0x32,
        (byte)0x30, (byte)0x36, (byte)0x31, (byte)0x00, (byte)0x00, (byte)0x00, (byte)0x78, (byte)0xA9,
        (byte)0x0B, (byte)0x8D, (byte)0x11, (byte)0xD0, (byte)0xA2, (byte)0xCC, (byte)0xBD, (byte)0x1F,
        (byte)0x08, (byte)0x95, (byte)0x00, (byte)0xCA, (byte)0xD0, (byte)0xF8, (byte)0x4C, (byte)0x02,
        (byte)0x00, (byte)0x34, (byte)0xBD, (byte)0x00, (byte)0x10, (byte)0x9D, (byte)0x00, (byte)0xFF,
        (byte)0xE8, (byte)0xD0, (byte)0xF7, (byte)0xC6, (byte)0x07, (byte)0xA9, (byte)0x06, (byte)0xC7,
        (byte)0x04, (byte)0x90, (byte)0xEF, (byte)0xA0, (byte)0x00, (byte)0xB3, (byte)0x24, (byte)0x30,
        (byte)0x29, (byte)0xC9, (byte)0x20, (byte)0xB0, (byte)0x47, (byte)0xE6, (byte)0x24, (byte)0xD0,
        (byte)0x02, (byte)0xE6, (byte)0x25, (byte)0xB9, (byte)0xFF, (byte)0xFF, (byte)0x99, (byte)0xFF,
        (byte)0xFF, (byte)0xC8, (byte)0xCA, (byte)0xD0, (byte)0xF6, (byte)0x98, (byte)0xAA, (byte)0xA0,
        (byte)0x00, (byte)0x65, (byte)0x27, (byte)0x85, (byte)0x27, (byte)0xB0, (byte)0x74, (byte)0x8A,
        (byte)0x65, (byte)0x24, (byte)0x85, (byte)0x24, (byte)0x90, (byte)0xD7, (byte)0xE6, (byte)0x25,
        (byte)0xB0, (byte)0xD3, (byte)0x4B, (byte)0x7F, (byte)0x90, (byte)0x39, (byte)0xF0, (byte)0x68,
        (byte)0xA2, (byte)0x02, (byte)0x85, (byte)0x59, (byte)0xC8, (byte)0xB1, (byte)0x24, (byte)0xA4,
        (byte)0x59, (byte)0x91, (byte)0x27, (byte)0x88, (byte)0x91, (byte)0x27, (byte)0xD0, (byte)0xFB,
        (byte)0xA9, (byte)0x00, (byte)0xB0, (byte)0xD5, (byte)0xA9, (byte)0x37, (byte)0x85, (byte)0x01,
        (byte)0x58, (byte)0x4C, (byte)0x61, (byte)0x00, (byte)0xF0, (byte)0xF6, (byte)0x09, (byte)0x80,
        (byte)0x65, (byte)0x27, (byte)0x85, (byte)0xA0, (byte)0xA5, (byte)0x28, (byte)0xE9, (byte)0x00,
        (byte)0x85, (byte)0xA1, (byte)0xB1, (byte)0xA0, (byte)0x91, (byte)0x27, (byte)0xC8, (byte)0xB1,
        (byte)0xA0, (byte)0x91, (byte)0x27, (byte)0x98, (byte)0xAA, (byte)0xD0, (byte)0xB0, (byte)0x4A,
        (byte)0x85, (byte)0xA5, (byte)0xC8, (byte)0xA5, (byte)0x27, (byte)0x90, (byte)0x31, (byte)0xF1,
        (byte)0x24, (byte)0x85, (byte)0xA0, (byte)0xA5, (byte)0x28, (byte)0xE9, (byte)0x00, (byte)0x85,
        (byte)0xA1, (byte)0xA2, (byte)0x02, (byte)0xA0, (byte)0x00, (byte)0xB1, (byte)0xA0, (byte)0x91,
        (byte)0x27, (byte)0xC8, (byte)0xB1, (byte)0xA0, (byte)0x91, (byte)0x27, (byte)0xC8, (byte)0xB9,
        (byte)0xA0, (byte)0x00, (byte)0x91, (byte)0x27, (byte)0xC0, (byte)0x00, (byte)0xD0, (byte)0xF6,
        (byte)0x98, (byte)0xB0, (byte)0x84, (byte)0xE6, (byte)0x28, (byte)0x18, (byte)0x90, (byte)0x87,
        (byte)0xA0, (byte)0xFF, (byte)0x84, (byte)0x59, (byte)0xA2, (byte)0x01, (byte)0xD0, (byte)0x99,
        (byte)0x71, (byte)0x24, (byte)0x85, (byte)0xA0, (byte)0xC8, (byte)0xB3, (byte)0x24, (byte)0x09,
        (byte)0x80, (byte)0x65, (byte)0x28, (byte)0x85, (byte)0xA1, (byte)0xE0, (byte)0x80, (byte)0x26,
        (byte)0xA5, (byte)0xA2, (byte)0x03, (byte)0xD0, (byte)0xC6
    };

    private static final byte[] BOOT2 = new byte[] {
        (byte)0x01, (byte)0x08, (byte)0x0B, (byte)0x08, (byte)0x0A, (byte)0x00, (byte)0x9E, (byte)0x32,
        (byte)0x30, (byte)0x36, (byte)0x31, (byte)0x00, (byte)0x00, (byte)0x00, (byte)0x78, (byte)0xA9,
        (byte)0x34, (byte)0x85, (byte)0x01, (byte)0xA2, (byte)0xD0, (byte)0xBD, (byte)0x1F, (byte)0x08,
        (byte)0x9D, (byte)0xFB, (byte)0x00, (byte)0xCA, (byte)0xD0, (byte)0xF7, (byte)0x4C, (byte)0x00,
        (byte)0x01, (byte)0xAA, (byte)0xAA, (byte)0xAA, (byte)0xAA, (byte)0xBD, (byte)0x00, (byte)0x10,
        (byte)0x9D, (byte)0x00, (byte)0xFF, (byte)0xE8, (byte)0xD0, (byte)0xF7, (byte)0xCE, (byte)0x05,
        (byte)0x01, (byte)0xA9, (byte)0x06, (byte)0xCF, (byte)0x02, (byte)0x01, (byte)0x90, (byte)0xED,
        (byte)0xA0, (byte)0x00, (byte)0xB3, (byte)0xFC, (byte)0x30, (byte)0x27, (byte)0xC9, (byte)0x20,
        (byte)0xB0, (byte)0x45, (byte)0xE6, (byte)0xFC, (byte)0xD0, (byte)0x02, (byte)0xE6, (byte)0xFD,
        (byte)0xB1, (byte)0xFC, (byte)0x91, (byte)0xFE, (byte)0xC8, (byte)0xCA, (byte)0xD0, (byte)0xF8,
        (byte)0x98, (byte)0xAA, (byte)0xA0, (byte)0x00, (byte)0x65, (byte)0xFE, (byte)0x85, (byte)0xFE,
        (byte)0xB0, (byte)0x74, (byte)0x8A, (byte)0x65, (byte)0xFC, (byte)0x85, (byte)0xFC, (byte)0x90,
        (byte)0xD9, (byte)0xE6, (byte)0xFD, (byte)0xB0, (byte)0xD5, (byte)0x4B, (byte)0x7F, (byte)0x90,
        (byte)0x39, (byte)0xF0, (byte)0x68, (byte)0xA2, (byte)0x02, (byte)0x85, (byte)0xF9, (byte)0xC8,
        (byte)0xB1, (byte)0xFC, (byte)0xA4, (byte)0xF9, (byte)0x91, (byte)0xFE, (byte)0x88, (byte)0x91,
        (byte)0xFE, (byte)0xD0, (byte)0xFB, (byte)0xA5, (byte)0xF9, (byte)0xB0, (byte)0xD5, (byte)0xA9,
        (byte)0x37, (byte)0x85, (byte)0x01, (byte)0x58, (byte)0x4C, (byte)0x5F, (byte)0x01, (byte)0xF0,
        (byte)0xF6, (byte)0x09, (byte)0x80, (byte)0x65, (byte)0xFE, (byte)0x85, (byte)0xFA, (byte)0xA5,
        (byte)0xFF, (byte)0xE9, (byte)0x00, (byte)0x85, (byte)0xFB, (byte)0xB1, (byte)0xFA, (byte)0x91,
        (byte)0xFE, (byte)0xC8, (byte)0xB1, (byte)0xFA, (byte)0x91, (byte)0xFE, (byte)0x98, (byte)0xAA,
        (byte)0xD0, (byte)0xB0, (byte)0x4A, (byte)0x8D, (byte)0xA3, (byte)0x01, (byte)0xC8, (byte)0xA5,
        (byte)0xFE, (byte)0x90, (byte)0x30, (byte)0xF1, (byte)0xFC, (byte)0x85, (byte)0xFA, (byte)0xA5,
        (byte)0xFF, (byte)0xE9, (byte)0x00, (byte)0x85, (byte)0xFB, (byte)0xA2, (byte)0x02, (byte)0xA0,
        (byte)0x00, (byte)0xB1, (byte)0xFA, (byte)0x91, (byte)0xFE, (byte)0xC8, (byte)0xB1, (byte)0xFA,
        (byte)0x91, (byte)0xFE, (byte)0xC8, (byte)0xB1, (byte)0xFA, (byte)0x91, (byte)0xFE, (byte)0xC0,
        (byte)0x00, (byte)0xD0, (byte)0xF7, (byte)0x98, (byte)0xB0, (byte)0x84, (byte)0xE6, (byte)0xFF,
        (byte)0x18, (byte)0x90, (byte)0x87, (byte)0xA0, (byte)0xAA, (byte)0x84, (byte)0xF9, (byte)0xA2,
        (byte)0x01, (byte)0xD0, (byte)0x99, (byte)0x71, (byte)0xFC, (byte)0x85, (byte)0xFA, (byte)0xC8,
        (byte)0xB3, (byte)0xFC, (byte)0x09, (byte)0x80, (byte)0x65, (byte)0xFF, (byte)0x85, (byte)0xFB,
        (byte)0xE0, (byte)0x80, (byte)0x2E, (byte)0xA3, (byte)0x01, (byte)0xA2, (byte)0x03, (byte)0xD0,
        (byte)0xC6
    };


    private enum TokenType {
        LITERAL,
        RLE,
        LZ,
        LZ2,
        ZERORUN
    }

    private static class Token {
        TokenType type;
        int pos;
        int size;
        int offset;
        int rlebyte;
    }

    private static class LZCandidates {
        Token shortMatch;
        Token longMatch;
    }

    private static class Options {
        boolean quiet;
        boolean prg;
        boolean sfx;
        boolean blank;
        boolean inplace;
        boolean selfcheck;
        int sfxmode;
        int jmp;
    }

    private static class ByteBuilder {
        byte[] buf = new byte[0];
        int len = 0;

        void appendByte(int v) {
            if (len + 1 > buf.length) {
                int newCap = buf.length == 0 ? 256 : buf.length * 2;
                buf = Arrays.copyOf(buf, newCap);
            }
            buf[len++] = (byte)(v & 0xFF);
        }

        void appendBytes(byte[] src, int off, int count) {
            if (count <= 0) {
                return;
            }
            if (len + count > buf.length) {
                int newCap = buf.length == 0 ? 256 : buf.length * 2;
                while (newCap < len + count) {
                    newCap *= 2;
                }
                buf = Arrays.copyOf(buf, newCap);
            }
            System.arraycopy(src, off, buf, len, count);
            len += count;
        }

        byte[] toArray() {
            return Arrays.copyOf(buf, len);
        }
    }


    private static void usage() {
        System.out.println("TSCrunch 1.3.2 - binary cruncher, by Antonio Savona");
        System.out.println("Usage: tscrunch [-p] [-i] [-r] [-q] [-x[2] $addr] [--selfcheck] infile outfile");
        System.out.println(" -p  : input file is a prg, first 2 bytes are discarded");
        System.out.println(" -x  $addr: creates a self extracting file (forces -p)");
        System.out.println(" -x2 $addr: creates a self extracting file with sfx code in stack (forces -p)");
        System.out.println(" -b  : blanks screen during decrunching (only with -x)");
        System.out.println(" -i  : inplace crunching (forces -p)");
        System.out.println(" -q  : quiet mode");
        System.out.println(" --selfcheck: compare output size against the Go encoder");
    }

    private static int minInt(int a, int b) {
        return a < b ? a : b;
    }

    private static int maxInt(int a, int b) {
        return a > b ? a : b;
    }

    private static int findOptimalZero(byte[] src) {
        int[] counts = new int[257];
        int[] firstSeen = new int[257];
        Arrays.fill(firstSeen, -1);
        int i = 0;
        int order = 0;

        while (i < src.length - 1) {
            if (src[i] == 0) {
                int j = i + 1;
                while (j < src.length && src[j] == 0 && (j - i) < 256) {
                    j++;
                }
                int run = j - i;
                if (run >= MINRLE && run <= 256) {
                    if (firstSeen[run] < 0) {
                        firstSeen[run] = order++;
                    }
                    counts[run]++;
                }
                i = j;
            } else {
                i++;
            }
        }

        int bestRun = LONGESTRLE;
        double bestScore = 0.0;
        int bestFirst = Integer.MAX_VALUE;
        for (int run = MINRLE; run <= 256; run++) {
            if (counts[run] > 0) {
                double score = (double)run * Math.pow((double)counts[run], 1.1);
                if (score > bestScore || (score == bestScore && firstSeen[run] >= 0 && firstSeen[run] < bestFirst)) {
                    bestScore = score;
                    bestRun = run;
                    bestFirst = firstSeen[run];
                }
            }
        }
        return bestRun;
    }

    private static int rleLength(byte[] src, int pos) {
        int x = 0;
        while (pos + x < src.length && x < LONGESTRLE + 1 && src[pos + x] == src[pos]) {
            x++;
        }
        return x;
    }

    private static int lz2Offset(byte[] src, int pos) {
        if (pos + LZ2SIZE > src.length) {
            return -1;
        }
        int start = pos - LZ2OFFSET;
        if (start < 0) {
            start = 0;
        }
        for (int j = pos - 1; j >= start; j--) {
            if (src[j] == src[pos] && src[j + 1] == src[pos + 1]) {
                return pos - j;
            }
        }
        return -1;
    }

    private static Token makeLz(int pos, int size, int offset) {
        Token t = new Token();
        t.type = TokenType.LZ;
        t.pos = pos;
        t.size = size;
        t.offset = offset;
        t.rlebyte = 0;
        return t;
    }

    // For every position, store the nearest earlier position with the same
    // three-byte prefix. Match enumeration then visits only real prefix
    // occurrences inside the LZ window.
    private static int[] buildPrefixPrevious(byte[] src) {
        int[] previous = new int[src.length];
        Arrays.fill(previous, -1);
        if (src.length < MINLZ) {
            return previous;
        }

        int entries = src.length - MINLZ + 1;
        int capacity = 16;
        while (capacity < entries * 2) {
            capacity <<= 1;
        }
        int[] keys = new int[capacity];
        int[] heads = new int[capacity];
        Arrays.fill(keys, -1);
        Arrays.fill(heads, -1);
        int mask = capacity - 1;

        for (int i = 0; i + MINLZ <= src.length; i++) {
            int key = ((src[i] & 0xff) << 16) |
                      ((src[i + 1] & 0xff) << 8) |
                      (src[i + 2] & 0xff);
            int slot = (key * 0x9e3779b1) & mask;
            while (keys[slot] != -1 && keys[slot] != key) {
                slot = (slot + 1) & mask;
            }
            if (keys[slot] == -1) {
                keys[slot] = key;
            }
            previous[i] = heads[slot];
            heads[slot] = i;
        }
        return previous;
    }

    // Keep the two encoded cost classes independent. A nearby match can use
    // the two-byte token through length 32, while every valid offset can use
    // the three-byte token through length 64. Equal-length ties choose the
    // nearest source explicitly.
    private static LZCandidates findLzCandidates(byte[] src, int pos, int minlz, int[] prefixPrevious) {
        LZCandidates candidates = new LZCandidates();
        candidates.shortMatch = makeLz(pos, 0, 0);
        candidates.longMatch = makeLz(pos, 0, 0);

        if (src.length - pos < minlz) {
            return candidates;
        }

        int x0 = pos - LONGLZOFFSET;
        if (x0 < 0) {
            x0 = 0;
        }

        for (int j = prefixPrevious[pos]; j >= x0; j = prefixPrevious[j]) {
            boolean match = true;
            for (int k = 0; k < minlz; k++) {
                if (src[j + k] != src[pos + k]) {
                    match = false;
                    break;
                }
            }
            if (!match) {
                continue;
            }

            int offset = pos - j;
            if (offset < MINLZOFFSET) {
                continue;
            }
            int l = minlz;
            while (pos + l < src.length && l < LONGESTLONGLZ && src[j + l] == src[pos + l]) {
                l++;
            }

            if (offset < LZOFFSET) {
                int shortLength = minInt(l, LONGESTLZ);
                if (shortLength > candidates.shortMatch.size ||
                    (shortLength == candidates.shortMatch.size && offset < candidates.shortMatch.offset)) {
                    candidates.shortMatch = makeLz(pos, shortLength, offset);
                }
            }

            if (l > candidates.longMatch.size ||
                (l == candidates.longMatch.size && offset < candidates.longMatch.offset)) {
                candidates.longMatch = makeLz(pos, l, offset);
            }
        }
        return candidates;
    }

    private static boolean zeroRunAt(byte[] src, int pos, int run) {
        if (run <= 0) {
            return false;
        }
        if (pos + run > src.length) {
            return false;
        }
        for (int i = 0; i < run; i++) {
            if (src[pos + i] != 0) {
                return false;
            }
        }
        return true;
    }

    private static boolean lzIsLong(Token t) {
        return (t.offset >= LZOFFSET) || (t.size > LONGESTLZ);
    }

    private static long tokenCost(Token t) {
        long mdiv = (long)LONGESTLITERAL * 65536L;
        long size = t.size;
        switch (t.type) {
            case LZ:
                if (lzIsLong(t)) {
                    return mdiv * 3 + 138 - size;
                }
                return mdiv * 2 + 134 - size;
            case RLE:
                return mdiv * 2 + 128 - size;
            case ZERORUN:
                return mdiv * 1;
            case LZ2:
                return mdiv * 1 + 132 - size;
            case LITERAL:
                return mdiv * (size + 1) + 130 - size;
            default:
                return mdiv * 10;
        }
    }

    private static int payloadLen(Token t) {
        switch (t.type) {
            case LITERAL:
                return 1 + t.size;
            case RLE:
                return 2;
            case ZERORUN:
                return 1;
            case LZ2:
                return 1;
            case LZ:
                return lzIsLong(t) ? 3 : 2;
            default:
                return 0;
        }
    }

    private static void emitToken(ByteBuilder out, byte[] src, Token t) {
        switch (t.type) {
            case LITERAL:
                out.appendByte(LITERALMASK | (t.size & 0x1f));
                out.appendBytes(src, t.pos, t.size);
                break;
            case RLE:
                out.appendByte(RLEMASK | (((t.size - 1) << 1) & 0x7f));
                out.appendByte(t.rlebyte);
                break;
            case ZERORUN:
                out.appendByte(RLEMASK);
                break;
            case LZ2:
                out.appendByte(LZ2MASK | (127 - t.offset));
                break;
            case LZ:
                if (lzIsLong(t)) {
                    int neg = 0 - t.offset;
                    out.appendByte(LZMASK | ((((t.size - 1) >> 1) << 2) & 0x7f));
                    out.appendByte(neg & 0xff);
                    out.appendByte(((neg >> 8) & 0x7f) | (((t.size - 1) & 1) << 7));
                } else {
                    out.appendByte(LZMASK | (((t.size - 1) << 2) & 0x7f) | 2);
                    out.appendByte(t.offset & 0xff);
                }
                break;
            default:
                break;
        }
    }

    private static Token[] buildCandidateRow(byte[] src, int pos, int optimalRun,
                                             int[] prefixPrevious) {
        boolean[] present = new boolean[257];
        Token[] tokens = new Token[257];
        int maxSize = 0;

        int rleSize = rleLength(src, pos);
        int rleCap = minInt(rleSize, LONGESTRLE);
        LZCandidates lz = new LZCandidates();
        lz.shortMatch = makeLz(pos, 0, 0);
        lz.longMatch = makeLz(pos, 0, 0);
        if (rleCap < LONGESTLONGLZ - 1) {
            int minlz = maxInt(rleCap + 1, MINLZ);
            lz = findLzCandidates(src, pos, minlz, prefixPrevious);
        }

        for (int size = lz.shortMatch.size; size >= MINLZ && size > rleCap; size--) {
            Token t = makeLz(pos, size, lz.shortMatch.offset);
            tokens[size] = t;
            present[size] = true;
            maxSize = maxInt(maxSize, size);
        }
        int longMinimum = maxInt(maxInt(MINLZ, rleCap + 1), lz.shortMatch.size + 1);
        for (int size = lz.longMatch.size; size >= longMinimum; size--) {
            Token t = makeLz(pos, size, lz.longMatch.offset);
            tokens[size] = t;
            present[size] = true;
            maxSize = maxInt(maxSize, size);
        }

        if (rleSize > LONGESTRLE) {
            Token t = new Token();
            t.type = TokenType.RLE;
            t.pos = pos;
            t.size = LONGESTRLE;
            t.rlebyte = src[pos] & 0xff;
            tokens[t.size] = t;
            present[t.size] = true;
            maxSize = maxInt(maxSize, t.size);
        } else {
            for (int size = rleSize; size >= MINRLE; size--) {
                Token t = new Token();
                t.type = TokenType.RLE;
                t.pos = pos;
                t.size = size;
                t.rlebyte = src[pos] & 0xff;
                tokens[size] = t;
                present[size] = true;
                maxSize = maxInt(maxSize, size);
            }
        }

        int lz2 = lz2Offset(src, pos);
        if (lz2 > 0) {
            Token t = new Token();
            t.type = TokenType.LZ2;
            t.pos = pos;
            t.size = LZ2SIZE;
            t.offset = lz2;
            tokens[t.size] = t;
            present[t.size] = true;
            maxSize = maxInt(maxSize, t.size);
        }
        if (zeroRunAt(src, pos, optimalRun)) {
            Token t = new Token();
            t.type = TokenType.ZERORUN;
            t.pos = pos;
            t.size = optimalRun;
            if (t.size <= 256) {
                tokens[t.size] = t;
                present[t.size] = true;
                maxSize = maxInt(maxSize, t.size);
            }
        }

        int count = 0;
        for (int size = 1; size <= maxSize; size++) {
            if (present[size]) {
                count++;
            }
        }
        Token[] row = new Token[count];
        int index = 0;
        for (int size = 1; size <= maxSize; size++) {
            if (present[size]) {
                row[index++] = tokens[size];
            }
        }
        return row;
    }

    private static byte[] crunch(byte[] src, Options opt, byte[] addr, int[] optimalRunOut) {
        if (src == null || src.length <= 0) {
            return null;
        }

        byte[] workSrc = src;
        int workLen = src.length;
        byte remainderByte = 0;

        if (opt.inplace) {
            remainderByte = workSrc[workLen - 1];
            workLen -= 1;
            workSrc = Arrays.copyOf(workSrc, workLen);
        }

        int optimalRun = findOptimalZero(workSrc);
        optimalRunOut[0] = optimalRun;

        int[] prefixPrevious = buildPrefixPrevious(workSrc);
        int[] starts = new int[workLen + 1];
        ArrayList<Token> transitions = new ArrayList<>();
        Token[][] rows = new Token[workLen][];
        final byte[] candidateSource = workSrc;
        final int candidateOptimalRun = optimalRun;
        IntStream.range(0, workLen).parallel().forEach(i ->
            rows[i] = buildCandidateRow(candidateSource, i, candidateOptimalRun, prefixPrevious));
        for (int i = 0; i < workLen; i++) {
            starts[i] = transitions.size();
            for (Token transition : rows[i]) {
                transitions.add(transition);
            }
        }
        starts[workLen] = transitions.size();

        int n = workLen;
        long[] dist = new long[n + 1];
        int[] prev = new int[n + 1];
        Token[] prevToken = new Token[n + 1];

        Arrays.fill(dist, Long.MAX_VALUE / 4);
        Arrays.fill(prev, -1);
        dist[0] = 0;

        // All edges move forward, so positions are a topological order.
        // Missing literal transitions are generated on demand.
        long mdiv = (long)LONGESTLITERAL * 65536L;
        for (int u = 0; u < n; u++) {
            if (dist[u] == Long.MAX_VALUE / 4) {
                continue;
            }
            int specializedLiteralLengths = 0;
            for (int ti = starts[u]; ti < starts[u + 1]; ti++) {
                Token transition = transitions.get(ti);
                int v = u + transition.size;
                long alt = dist[u] + tokenCost(transition);
                boolean better = alt < dist[v];
                if (alt == dist[v] && prev[v] >= 0) {
                    better = dist[u] < dist[prev[v]];
                }
                if (better) {
                    dist[v] = alt;
                    prev[v] = u;
                    prevToken[v] = transition;
                }
                if (transition.size <= LONGESTLITERAL) {
                    specializedLiteralLengths |= 1 << transition.size;
                }
            }

            int literalMax = minInt(LONGESTLITERAL, n - u);
            for (int size = 1; size <= literalMax; size++) {
                if ((specializedLiteralLengths & (1 << size)) != 0) {
                    continue;
                }
                int v = u + size;
                long alt = dist[u] + mdiv * (size + 1L) + 130L - size;
                boolean better = alt < dist[v];
                if (alt == dist[v] && prev[v] >= 0) {
                    better = dist[u] < dist[prev[v]];
                }
                if (better) {
                    Token literal = new Token();
                    literal.type = TokenType.LITERAL;
                    literal.pos = u;
                    literal.size = size;
                    dist[v] = alt;
                    prev[v] = u;
                    prevToken[v] = literal;
                }
            }
        }

        if (prev[n] < 0) {
            return null;
        }

        int tokenCount = 0;
        for (int v = n; v > 0; v = prev[v]) {
            tokenCount++;
        }

        Token[] tokenList = new Token[tokenCount];
        int idx = tokenCount - 1;
        for (int v = n; v > 0; v = prev[v]) {
            tokenList[idx--] = prevToken[v];
        }

        ByteBuilder out = new ByteBuilder();
        if (opt.inplace) {
            int safety = tokenCount;
            int segmentUncrunched = 0;
            int segmentCrunched = 0;
            int totalUncrunched = 0;

            for (int i = tokenCount - 1; i >= 0; i--) {
                segmentCrunched += payloadLen(tokenList[i]);
                segmentUncrunched += tokenList[i].size;
                if (segmentUncrunched <= segmentCrunched) {
                    safety = i;
                    totalUncrunched += segmentUncrunched;
                    segmentUncrunched = 0;
                    segmentCrunched = 0;
                }
            }

            byte[] remainder;
            int remainderLen = 1;
            if (totalUncrunched > 0) {
                remainderLen = totalUncrunched + 1;
                remainder = new byte[remainderLen];
                System.arraycopy(workSrc, workLen - totalUncrunched, remainder, 0, totalUncrunched);
                remainder[totalUncrunched] = remainderByte;
            } else {
                remainder = new byte[] { remainderByte };
            }

            for (int i = 0; i < safety; i++) {
                emitToken(out, workSrc, tokenList[i]);
            }
            out.appendByte(TERMINATOR);
            if (remainderLen > 1) {
                out.appendBytes(remainder, 1, remainderLen - 1);
            }

            ByteBuilder finalOut = new ByteBuilder();
            finalOut.appendBytes(addr, 0, 2);
            finalOut.appendByte(optimalRun - 1);
            finalOut.appendByte(remainder[0] & 0xff);
            byte[] payload = out.toArray();
            finalOut.appendBytes(payload, 0, payload.length);
            return finalOut.toArray();
        }

        if (!opt.sfx) {
            out.appendByte(optimalRun - 1);
        }
        for (Token token : tokenList) {
            emitToken(out, workSrc, token);
        }
        out.appendByte(TERMINATOR);
        return out.toArray();
    }

    private static boolean parseJmp(String s, Options opt) {
        if (s == null || s.isEmpty()) {
            return false;
        }
        String p = s;
        if (p.startsWith("$")) {
            p = p.substring(1);
        } else if (p.startsWith("0x") || p.startsWith("0X")) {
            p = p.substring(2);
        }
        try {
            int val = Integer.parseInt(p, 16);
            if (val < 0 || val > 0xFFFF) {
                return false;
            }
            opt.jmp = val;
            return true;
        } catch (NumberFormatException ex) {
            return false;
        }
    }

    private static void runCommand(String cmd) {
        try {
            String os = System.getProperty("os.name").toLowerCase();
            ProcessBuilder pb;
            if (os.contains("win")) {
                pb = new ProcessBuilder("cmd.exe", "/c", cmd);
            } else {
                pb = new ProcessBuilder("sh", "-c", cmd);
            }
            pb.inheritIO();
            Process p = pb.start();
            p.waitFor();
        } catch (IOException | InterruptedException ex) {
            System.err.println("Selfcheck: command failed: " + cmd);
        }
    }

    private static long fileSize(String path) {
        try {
            return Files.size(Paths.get(path));
        } catch (IOException ex) {
            return -1;
        }
    }



    public static void main(String[] args) {
        Options opt = new Options();
        String jmpArg = null;
        boolean useX2 = false;

        if (args.length < 3) {
            usage();
            return;
        }

        for (String arg : args) {
            if ("-h".equals(arg)) {
                usage();
                return;
            }
        }

        for (int i = 0; i < args.length - 2; i++) {
            String arg = args[i];
            if ("-q".equals(arg)) {
                opt.quiet = true;
            } else if ("--selfcheck".equals(arg)) {
                opt.selfcheck = true;
            } else if ("-p".equals(arg)) {
                opt.prg = true;
            } else if ("-i".equals(arg)) {
                opt.inplace = true;
                opt.prg = true;
            } else if ("-b".equals(arg)) {
                opt.blank = true;
            } else if ("-x".equals(arg) || "-x2".equals(arg)) {
                boolean isX2 = "-x2".equals(arg);
                if (i + 1 >= args.length) {
                    usage();
                    return;
                }
                opt.sfx = true;
                opt.sfxmode = isX2 ? 1 : 0;
                opt.prg = true;
                if (!parseJmp(args[i + 1], opt)) {
                    System.err.println("Invalid jump address: " + args[i + 1]);
                    return;
                }
                jmpArg = args[i + 1];
                useX2 = isX2;
                i++;
            }
        }

        if (opt.sfx && opt.inplace) {
            System.err.println("Can't create an sfx prg with inplace crunching");
            return;
        }

        String inPath = args[args.length - 2];
        String outPath = args[args.length - 1];

        byte[] src;
        try {
            src = Files.readAllBytes(Paths.get(inPath));
        } catch (IOException ex) {
            System.err.println("Failed to read input file");
            return;
        }

        int sourceLen = src.length;
        byte[] crunchSrc = src;
        int crunchLen = src.length;
        byte[] addr = new byte[] {0, 0};
        int decrunchTo = 0;
        int loadTo = 0;

        if (opt.prg) {
            if (crunchLen < 2) {
                System.err.println("Input too small for PRG");
                return;
            }
            addr[0] = crunchSrc[0];
            addr[1] = crunchSrc[1];
            decrunchTo = (addr[0] & 0xff) + 256 * (addr[1] & 0xff);
            crunchSrc = Arrays.copyOfRange(crunchSrc, 2, crunchSrc.length);
            crunchLen -= 2;
        }

        int[] optimalRunOut = new int[] { LONGESTRLE };
        byte[] crunched = crunch(crunchSrc, opt, addr, optimalRunOut);
        if (crunched == null) {
            System.err.println("Crunch failed");
            return;
        }
        int crunchedLen = crunched.length;
        int optimalRun = optimalRunOut[0];

        if (opt.sfx) {
byte[] bootSrc;
            int gap = 0;
            if (opt.sfxmode == 0) {
                if (opt.blank) {
                    bootSrc = BLANK_BOOT;
                    gap = 5;
                } else {
                    bootSrc = BOOT;
                    gap = 0;
                }
            } else {
                bootSrc = BOOT2;
                gap = 0;
            }

            byte[] bootBuf = Arrays.copyOf(bootSrc, bootSrc.length);
            int fileLen = bootBuf.length + crunchedLen;
            int startAddress = 0x10000 - crunchedLen;
            int transfAddress = fileLen + 0x6ff;

            if (opt.sfxmode == 0) {
                bootBuf[0x1e + gap] = (byte)(transfAddress & 0xff);
                bootBuf[0x1f + gap] = (byte)(transfAddress >> 8);

                bootBuf[0x3f + gap] = (byte)(startAddress & 0xff);
                bootBuf[0x40 + gap] = (byte)(startAddress >> 8);

                bootBuf[0x42 + gap] = (byte)(decrunchTo & 0xff);
                bootBuf[0x43 + gap] = (byte)(decrunchTo >> 8);

                bootBuf[0x7d + gap] = (byte)(opt.jmp & 0xff);
                bootBuf[0x7e + gap] = (byte)(opt.jmp >> 8);

                bootBuf[0xcc + gap] = (byte)(optimalRun - 1);
            } else {
                bootBuf[0x26] = (byte)(transfAddress & 0xff);
                bootBuf[0x27] = (byte)(transfAddress >> 8);

                bootBuf[0x21] = (byte)(startAddress & 0xff);
                bootBuf[0x22] = (byte)(startAddress >> 8);

                bootBuf[0x23] = (byte)(decrunchTo & 0xff);
                bootBuf[0x24] = (byte)(decrunchTo >> 8);

                bootBuf[0x85] = (byte)(opt.jmp & 0xff);
                bootBuf[0x86] = (byte)(opt.jmp >> 8);

                bootBuf[0xd4] = (byte)(optimalRun - 1);
            }

            byte[] finalOut = new byte[bootBuf.length + crunchedLen];
            System.arraycopy(bootBuf, 0, finalOut, 0, bootBuf.length);
            System.arraycopy(crunched, 0, finalOut, bootBuf.length, crunchedLen);
            crunched = finalOut;
            crunchedLen = finalOut.length;
            loadTo = 0x0801;
        }

        int decrunchEnd = (decrunchTo + crunchLen - 1) & 0xffff;

        if (opt.inplace) {
            loadTo = (decrunchEnd - crunchedLen + 1) & 0xffff;
            byte[] finalOut = new byte[crunchedLen + 2];
            finalOut[0] = (byte)(loadTo & 0xff);
            finalOut[1] = (byte)(loadTo >> 8);
            System.arraycopy(crunched, 0, finalOut, 2, crunchedLen);
            crunched = finalOut;
        }

        try {
            Files.write(Paths.get(outPath), crunched);
        } catch (IOException ex) {
            System.err.println("Failed to write output file");
            return;
        }

        if (!opt.quiet) {
            double ratio = (double)crunchedLen * 100.0 / (double)sourceLen;
            System.out.printf("input file  %s: %s, $%04x - $%04x : %d bytes%n",
                opt.prg ? "PRG" : "RAW", inPath, decrunchTo & 0xffff, decrunchEnd & 0xffff, sourceLen);
            System.out.printf("output file %s: %s, $%04x - $%04x : %d bytes%n",
                (opt.sfx || opt.inplace) ? "PRG" : "RAW", outPath, loadTo & 0xffff,
                (loadTo + crunchedLen - 1) & 0xffff, crunchedLen);
            System.out.printf("crunched to %.2f%% of original size%n", ratio);
        }

        if (opt.selfcheck) {
            String outGo = outPath + ".go";

            StringBuilder flags = new StringBuilder("-q");
            if (opt.inplace) {
                flags.append(" -i");
            } else if (opt.prg && !opt.sfx) {
                flags.append(" -p");
            }
            if (opt.blank) {
                flags.append(" -b");
            }
            if (opt.sfx) {
                if (jmpArg == null) {
                    jmpArg = String.format("$%04x", opt.jmp & 0xffff);
                }
                if (useX2) {
                    flags.append(" -x2 ").append(jmpArg);
                } else {
                    flags.append(" -x ").append(jmpArg);
                }
            }

            runCommand(String.format("go run tscrunch.go %s \"%s\" \"%s\"", flags, inPath, outGo));

            long szJava = fileSize(outPath);
            long szGo = fileSize(outGo);
            System.out.printf("Selfcheck sizes (bytes): Java=%d Go=%d%n", szJava, szGo);
        }
    }
}




