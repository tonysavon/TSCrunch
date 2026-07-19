#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>
#include <math.h>
#include <limits.h>

#include "boot_code.h"

/*
 * TSCrunch v1.3.2 - C99 encoder
 * Drop-in CLI compatible with tscrunch.go
 */

#define LONGESTRLE      64
#define LONGESTLONGLZ   64
#define LONGESTLZ       32
#define LONGESTLITERAL  31
#define MINRLE          2
#define MINLZ           3
#define MINLZOFFSET     2
#define LZOFFSET        256
#define LONGLZOFFSET    32767
#define LZ2OFFSET       94
#define LZ2SIZE         2

#define RLEMASK         0x81
#define LZMASK          0x80
#define LITERALMASK     0x00
#define LZ2MASK         0x00

#define TERMINATOR      (LONGESTLITERAL + 1)

typedef enum {
    TOKEN_LITERAL = 0,
    TOKEN_RLE = 1,
    TOKEN_LZ = 2,
    TOKEN_LZ2 = 3,
    TOKEN_ZERORUN = 4
} TokenType;

typedef struct {
    TokenType type;
    int pos;
    int size;
    int offset;
    uint8_t rlebyte;
} Token;

typedef struct {
    Token short_match;
    Token long_match;
} LZCandidates;

typedef struct {
    int dest;
    int64_t cost;
    Token token;
} Edge;

typedef struct {
    Edge *edges;
    int count;
    int cap;
} EdgeList;

typedef struct {
    bool quiet;
    bool prg;
    bool sfx;
    bool blank;
    bool inplace;
    bool selfcheck;
    int sfxmode;
    uint16_t jmp;
} Options;

static int min_int(int a, int b) { return a < b ? a : b; }
static int max_int(int a, int b) { return a > b ? a : b; }

static void usage(void) {
    printf("TSCrunch 1.3.2 - binary cruncher, by Antonio Savona\n");
    printf("Usage: tscrunch [-p] [-i] [-r] [-q] [-x[2] $addr] [--selfcheck] infile outfile\n");
    printf(" -p  : input file is a prg, first 2 bytes are discarded\n");
    printf(" -x  $addr: creates a self extracting file (forces -p)\n");
    printf(" -x2 $addr: creates a self extracting file with sfx code in stack (forces -p)\n");
    printf(" -b  : blanks screen during decrunching (only with -x)\n");
    printf(" -i  : inplace crunching (forces -p)\n");
    printf(" -q  : quiet mode\n");
    printf(" --selfcheck: compare output size against the Go encoder\n");
}

static uint8_t *load_file(const char *path, size_t *out_len) {
    FILE *f = fopen(path, "rb");
    uint8_t *buf = NULL;
    size_t len = 0;

    if (!f) {
        return NULL;
    }
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        return NULL;
    }
    long sz = ftell(f);
    if (sz < 0) {
        fclose(f);
        return NULL;
    }
    if (fseek(f, 0, SEEK_SET) != 0) {
        fclose(f);
        return NULL;
    }
    len = (size_t)sz;
    buf = (uint8_t *)malloc(len);
    if (!buf) {
        fclose(f);
        return NULL;
    }
    if (fread(buf, 1, len, f) != len) {
        fclose(f);
        free(buf);
        return NULL;
    }
    fclose(f);
    *out_len = len;
    return buf;
}

static bool save_file(const char *path, const uint8_t *data, size_t len) {
    FILE *f = fopen(path, "wb");
    if (!f) {
        return false;
    }
    if (len > 0 && fwrite(data, 1, len, f) != len) {
        fclose(f);
        return false;
    }
    fclose(f);
    return true;
}

static long file_size(const char *path) {
    FILE *f = fopen(path, "rb");
    if (!f) {
        return -1;
    }
    if (fseek(f, 0, SEEK_END) != 0) {
        fclose(f);
        return -1;
    }
    long sz = ftell(f);
    fclose(f);
    return sz;
}

static void edge_list_add(EdgeList *list, Edge edge) {
    if (list->count >= list->cap) {
        int new_cap = list->cap == 0 ? 8 : list->cap * 2;
        Edge *new_edges = (Edge *)realloc(list->edges, (size_t)new_cap * sizeof(Edge));
        if (!new_edges) {
            return;
        }
        list->edges = new_edges;
        list->cap = new_cap;
    }
    list->edges[list->count++] = edge;
}

static int find_optimal_zero(const uint8_t *src, int len) {
    int counts[257];
    int first_seen[257];
    int i = 0;
    int order = 0;
    memset(counts, 0, sizeof(counts));
    for (int k = 0; k <= 256; k++) {
        first_seen[k] = -1;
    }

    while (i < len - 1) {
        if (src[i] == 0) {
            int j = i + 1;
            while (j < len && src[j] == 0 && (j - i) < 256) {
                j++;
            }
            int run = j - i;
            if (run >= MINRLE && run <= 256) {
                if (first_seen[run] < 0) {
                    first_seen[run] = order++;
                }
                counts[run]++;
            }
            i = j;
        } else {
            i++;
        }
    }

    int best_run = LONGESTRLE;
    double best_score = 0.0;
    int best_first = INT_MAX;
    for (int run = MINRLE; run <= 256; run++) {
        if (counts[run] > 0) {
            double score = (double)run * pow((double)counts[run], 1.1);
            if (score > best_score || (score == best_score && first_seen[run] >= 0 && first_seen[run] < best_first)) {
                best_score = score;
                best_run = run;
                best_first = first_seen[run];
            }
        }
    }

    return best_run;
}

static int rle_length(const uint8_t *src, int len, int pos) {
    int x = 0;
    while (pos + x < len && x < LONGESTRLE + 1 && src[pos + x] == src[pos]) {
        x++;
    }
    return x;
}

static int lz2_offset(const uint8_t *src, int len, int pos) {
    if (pos + LZ2SIZE > len) {
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

static Token make_lz(int pos, int size, int offset) {
    Token t;
    t.type = TOKEN_LZ;
    t.pos = pos;
    t.size = size;
    t.offset = offset;
    t.rlebyte = 0;
    return t;
}

/* For every position, store the nearest earlier position with the same
 * three-byte prefix. Building the chains is linear and match enumeration then
 * visits only genuine prefix occurrences inside the LZ window. */
static int *build_prefix_previous(const uint8_t *src, int len) {
    int *previous = (int *)malloc((size_t)len * sizeof(int));
    if (!previous) {
        return NULL;
    }
    for (int i = 0; i < len; i++) {
        previous[i] = -1;
    }
    if (len < MINLZ) {
        return previous;
    }

    int capacity = 16;
    while (capacity < (len - MINLZ + 1) * 2) {
        capacity <<= 1;
    }
    uint32_t *keys = (uint32_t *)malloc((size_t)capacity * sizeof(uint32_t));
    int *heads = (int *)malloc((size_t)capacity * sizeof(int));
    if (!keys || !heads) {
        free(keys);
        free(heads);
        free(previous);
        return NULL;
    }
    for (int i = 0; i < capacity; i++) {
        keys[i] = UINT32_MAX;
        heads[i] = -1;
    }

    for (int i = 0; i + MINLZ <= len; i++) {
        uint32_t key = ((uint32_t)src[i] << 16) |
                       ((uint32_t)src[i + 1] << 8) |
                       (uint32_t)src[i + 2];
        uint32_t slot = (key * UINT32_C(2654435761)) & (uint32_t)(capacity - 1);
        while (keys[slot] != UINT32_MAX && keys[slot] != key) {
            slot = (slot + 1) & (uint32_t)(capacity - 1);
        }
        if (keys[slot] == UINT32_MAX) {
            keys[slot] = key;
        }
        previous[i] = heads[slot];
        heads[slot] = i;
    }

    free(keys);
    free(heads);
    return previous;
}

/* Keep the two encoded cost classes independent. A nearby match can use the
 * two-byte token through length 32, while every valid offset can use the
 * three-byte token through length 64. Equal-length ties choose the nearest
 * source explicitly. */
static LZCandidates find_lz_candidates(const uint8_t *src, int len, int pos, int minlz,
                                       const int *prefix_previous) {
    LZCandidates candidates;
    candidates.short_match = make_lz(pos, 0, 0);
    candidates.long_match = make_lz(pos, 0, 0);

    if (len - pos < minlz) {
        return candidates;
    }

    int x0 = pos - LONGLZOFFSET;
    if (x0 < 0) {
        x0 = 0;
    }

    int j = prefix_previous ? prefix_previous[pos] : pos - 1;
    while (j >= x0) {
        if (memcmp(src + j, src + pos, (size_t)minlz) == 0) {
            int offset = pos - j;
            if (offset < MINLZOFFSET) {
                continue;
            }
            int l = minlz;
            while (pos + l < len && l < LONGESTLONGLZ && src[j + l] == src[pos + l]) {
                l++;
            }

            if (offset < LZOFFSET) {
                int short_length = min_int(l, LONGESTLZ);
                if (short_length > candidates.short_match.size ||
                    (short_length == candidates.short_match.size && offset < candidates.short_match.offset)) {
                    candidates.short_match = make_lz(pos, short_length, offset);
                }
            }

            if (l > candidates.long_match.size ||
                (l == candidates.long_match.size && offset < candidates.long_match.offset)) {
                candidates.long_match = make_lz(pos, l, offset);
            }
        }
        j = prefix_previous ? prefix_previous[j] : j - 1;
    }
    return candidates;
}

static bool zerorun_at(const uint8_t *src, int len, int pos, int run) {
    if (run <= 0) {
        return false;
    }
    if (pos + run > len) {
        return false;
    }
    for (int i = 0; i < run; i++) {
        if (src[pos + i] != 0) {
            return false;
        }
    }
    return true;
}

static bool lz_is_long(const Token *t) {
    return (t->offset >= LZOFFSET) || (t->size > LONGESTLZ);
}

static int64_t token_cost(const Token *t) {
    const int64_t mdiv = (int64_t)LONGESTLITERAL * 65536;
    int64_t size = (int64_t)t->size;

    switch (t->type) {
        case TOKEN_LZ:
            if (lz_is_long(t)) {
                return mdiv * 3 + 138 - size;
            }
            return mdiv * 2 + 134 - size;
        case TOKEN_RLE:
            return mdiv * 2 + 128 - size;
        case TOKEN_ZERORUN:
            return mdiv * 1;
        case TOKEN_LZ2:
            return mdiv * 1 + 132 - size;
        case TOKEN_LITERAL:
            return mdiv * (size + 1) + 130 - size;
        default:
            return mdiv * 10;
    }
}

static int payload_len(const Token *t) {
    switch (t->type) {
        case TOKEN_LITERAL:
            return 1 + t->size;
        case TOKEN_RLE:
            return 2;
        case TOKEN_ZERORUN:
            return 1;
        case TOKEN_LZ2:
            return 1;
        case TOKEN_LZ:
            return lz_is_long(t) ? 3 : 2;
        default:
            return 0;
    }
}

static void append_byte(uint8_t **buf, int *len, int *cap, uint8_t v) {
    if (*len + 1 > *cap) {
        int new_cap = (*cap == 0) ? 256 : (*cap * 2);
        uint8_t *new_buf = (uint8_t *)realloc(*buf, (size_t)new_cap);
        if (!new_buf) {
            return;
        }
        *buf = new_buf;
        *cap = new_cap;
    }
    (*buf)[(*len)++] = v;
}

static void append_bytes(uint8_t **buf, int *len, int *cap, const uint8_t *src, int count) {
    if (count <= 0) {
        return;
    }
    if (*len + count > *cap) {
        int new_cap = (*cap == 0) ? 256 : (*cap * 2);
        while (new_cap < *len + count) {
            new_cap *= 2;
        }
        uint8_t *new_buf = (uint8_t *)realloc(*buf, (size_t)new_cap);
        if (!new_buf) {
            return;
        }
        *buf = new_buf;
        *cap = new_cap;
    }
    memcpy(*buf + *len, src, (size_t)count);
    *len += count;
}

static void emit_token(uint8_t **buf, int *len, int *cap, const uint8_t *src, const Token *t) {
    switch (t->type) {
        case TOKEN_LITERAL:
            append_byte(buf, len, cap, (uint8_t)(LITERALMASK | (t->size & 0x1f)));
            append_bytes(buf, len, cap, src + t->pos, t->size);
            break;
        case TOKEN_RLE:
            append_byte(buf, len, cap, (uint8_t)(RLEMASK | (((t->size - 1) << 1) & 0x7f)));
            append_byte(buf, len, cap, t->rlebyte);
            break;
        case TOKEN_ZERORUN:
            append_byte(buf, len, cap, (uint8_t)RLEMASK);
            break;
        case TOKEN_LZ2:
            append_byte(buf, len, cap, (uint8_t)(LZ2MASK | (127 - t->offset)));
            break;
        case TOKEN_LZ: {
            if (lz_is_long(t)) {
                uint16_t neg = (uint16_t)(0 - t->offset);
                append_byte(buf, len, cap, (uint8_t)(LZMASK | ((((t->size - 1) >> 1) << 2) & 0x7f)));
                append_byte(buf, len, cap, (uint8_t)(neg & 0xff));
                append_byte(buf, len, cap, (uint8_t)(((neg >> 8) & 0x7f) | (((t->size - 1) & 1) << 7)));
            } else {
                append_byte(buf, len, cap, (uint8_t)(LZMASK | (((t->size - 1) << 2) & 0x7f) | 2));
                append_byte(buf, len, cap, (uint8_t)(t->offset & 0xff));
            }
            break;
        }
        default:
            break;
    }
}

static bool parse_jmp(const char *s, uint16_t *out) {
    if (!s || !*s) {
        return false;
    }

    int base = 16;
    const char *p = s;
    if (p[0] == '$') {
        p++;
    } else if (p[0] == '0' && (p[1] == 'x' || p[1] == 'X')) {
        p += 2;
    }

    char *end = NULL;
    long val = strtol(p, &end, base);
    if (!end || *end != '\0') {
        return false;
    }
    if (val < 0 || val > 0xFFFF) {
        return false;
    }
    *out = (uint16_t)val;
    return true;
}

static uint8_t *crunch(const uint8_t *src, int len, const Options *opt, const uint8_t addr[2], int *out_len, int *optimal_run) {
    if (!src || len <= 0 || !out_len || !optimal_run) {
        return NULL;
    }

    const uint8_t *work_src = src;
    int work_len = len;
    uint8_t remainder_byte = 0;

    if (opt->inplace) {
        remainder_byte = work_src[work_len - 1];
        work_len -= 1;
    }

    *optimal_run = find_optimal_zero(work_src, work_len);

    int *starts = (int *)malloc((size_t)(work_len + 1) * sizeof(int));
    if (!starts) {
        return NULL;
    }
    EdgeList transitions = {0};
    int *prefix_previous = build_prefix_previous(work_src, work_len);

    const int max_token_size = 256;

    for (int i = 0; i < work_len; i++) {
        starts[i] = transitions.count;
        bool present[257];
        Token tokens[257];
        int max_size = 0;

        memset(present, 0, sizeof(present));

        int rle_size = rle_length(work_src, work_len, i);
        int rle_cap = min_int(rle_size, LONGESTRLE);

        LZCandidates lz;
        lz.short_match = make_lz(i, 0, 0);
        lz.long_match = make_lz(i, 0, 0);
        if (rle_cap < LONGESTLONGLZ - 1) {
            int minlz = max_int(rle_cap + 1, MINLZ);
            lz = find_lz_candidates(work_src, work_len, i, minlz, prefix_previous);
        }

        for (int size = lz.short_match.size; size >= MINLZ && size > rle_cap; size--) {
            Token t = make_lz(i, size, lz.short_match.offset);
            tokens[t.size] = t;
            present[t.size] = true;
            if (t.size > max_size) {
                max_size = t.size;
            }
        }

        /* Long edges that end where a short edge ends are strictly more
         * expensive, so only expose destinations unavailable to short. */
        int long_minimum = max_int(max_int(MINLZ, rle_cap + 1), lz.short_match.size + 1);
        for (int size = lz.long_match.size; size >= long_minimum; size--) {
            Token t = make_lz(i, size, lz.long_match.offset);
            tokens[t.size] = t;
            present[t.size] = true;
            if (t.size > max_size) {
                max_size = t.size;
            }
        }

        if (rle_size > LONGESTRLE) {
            Token t;
            t.type = TOKEN_RLE;
            t.pos = i;
            t.size = LONGESTRLE;
            t.rlebyte = work_src[i];
            t.offset = 0;
            tokens[t.size] = t;
            present[t.size] = true;
            if (t.size > max_size) {
                max_size = t.size;
            }
        } else {
            for (int size = rle_size; size >= MINRLE; size--) {
                Token t;
                t.type = TOKEN_RLE;
                t.pos = i;
                t.size = size;
                t.rlebyte = work_src[i];
                t.offset = 0;
                tokens[t.size] = t;
                present[t.size] = true;
                if (t.size > max_size) {
                    max_size = t.size;
                }
            }
        }

        int lz2 = lz2_offset(work_src, work_len, i);
        if (lz2 > 0) {
            Token t;
            t.type = TOKEN_LZ2;
            t.pos = i;
            t.size = LZ2SIZE;
            t.offset = lz2;
            t.rlebyte = 0;
            tokens[t.size] = t;
            present[t.size] = true;
            if (t.size > max_size) {
                max_size = t.size;
            }
        }

        if (zerorun_at(work_src, work_len, i, *optimal_run)) {
            Token t;
            t.type = TOKEN_ZERORUN;
            t.pos = i;
            t.size = *optimal_run;
            t.offset = 0;
            t.rlebyte = 0;
            if (t.size <= max_token_size) {
                tokens[t.size] = t;
                present[t.size] = true;
                if (t.size > max_size) {
                    max_size = t.size;
                }
            }
        }

        for (int size = 1; size <= max_size; size++) {
            if (!present[size]) {
                continue;
            }
            if (size <= 0 || i + size > work_len) {
                continue;
            }
            Token t = tokens[size];
            Edge e;
            e.dest = i + size;
            e.token = t;
            e.cost = token_cost(&t);
            edge_list_add(&transitions, e);
        }
    }
    starts[work_len] = transitions.count;
    free(prefix_previous);

    int n = work_len;
    int64_t *dist = (int64_t *)malloc((size_t)(n + 1) * sizeof(int64_t));
    int *prev = (int *)malloc((size_t)(n + 1) * sizeof(int));
    Token *prev_token = (Token *)malloc((size_t)(n + 1) * sizeof(Token));

    if (!dist || !prev || !prev_token) {
        free(dist);
        free(prev);
        free(prev_token);
        free(transitions.edges);
        free(starts);
        return NULL;
    }

    for (int i = 0; i <= n; i++) {
        dist[i] = INT64_MAX / 4;
        prev[i] = -1;
    }
    dist[0] = 0;

    /* Every transition moves forward, so input positions are already a
     * topological order. Generate missing literal edges on demand rather than
     * storing them in the candidate graph. */
    for (int u = 0; u < n; u++) {
        if (dist[u] == INT64_MAX / 4) {
            continue;
        }
        uint32_t specialized_literal_lengths = 0;
        for (int ei = starts[u]; ei < starts[u + 1]; ei++) {
            Edge *edge = &transitions.edges[ei];
            int v = edge->dest;
            int64_t alt = dist[u] + edge->cost;
            bool better = alt < dist[v];
            if (alt == dist[v] && prev[v] >= 0) {
                better = dist[u] < dist[prev[v]];
            }
            if (better) {
                dist[v] = alt;
                prev[v] = u;
                prev_token[v] = edge->token;
            }
            if (edge->token.size <= LONGESTLITERAL) {
                specialized_literal_lengths |= UINT32_C(1) << edge->token.size;
            }
        }

        int literal_max = min_int(LONGESTLITERAL, n - u);
        for (int size = 1; size <= literal_max; size++) {
            if ((specialized_literal_lengths & (UINT32_C(1) << size)) != 0) {
                continue;
            }
            Token literal;
            literal.type = TOKEN_LITERAL;
            literal.pos = u;
            literal.size = size;
            literal.offset = 0;
            literal.rlebyte = 0;
            int v = u + size;
            int64_t alt = dist[u] + token_cost(&literal);
            bool better = alt < dist[v];
            if (alt == dist[v] && prev[v] >= 0) {
                better = dist[u] < dist[prev[v]];
            }
            if (better) {
                dist[v] = alt;
                prev[v] = u;
                prev_token[v] = literal;
            }
        }
    }

    if (prev[n] < 0) {
        free(transitions.edges);
        free(starts);
        free(dist);
        free(prev);
        free(prev_token);
        return NULL;
    }

    int token_count = 0;
    for (int v = n; v > 0; v = prev[v]) {
        token_count++;
    }

    Token *token_list = (Token *)malloc((size_t)token_count * sizeof(Token));
    if (!token_list) {
        free(transitions.edges);
        free(starts);
        free(dist);
        free(prev);
        free(prev_token);
        return NULL;
    }

    int idx = token_count - 1;
    for (int v = n; v > 0; v = prev[v]) {
        token_list[idx--] = prev_token[v];
    }

    uint8_t *out = NULL;
    int out_len_local = 0;
    int out_cap = 0;

    if (opt->inplace) {
        int safety = token_count;
        int segment_uncrunched = 0;
        int segment_crunched = 0;
        int total_uncrunched = 0;

        for (int i = token_count - 1; i >= 0; i--) {
            segment_crunched += payload_len(&token_list[i]);
            segment_uncrunched += token_list[i].size;
            if (segment_uncrunched <= segment_crunched) {
                safety = i;
                total_uncrunched += segment_uncrunched;
                segment_uncrunched = 0;
                segment_crunched = 0;
            }
        }

        uint8_t *remainder = NULL;
        int remainder_len = 1;
        if (total_uncrunched > 0) {
            remainder_len = total_uncrunched + 1;
            remainder = (uint8_t *)malloc((size_t)remainder_len);
            if (!remainder) {
                free(token_list);
                free(transitions.edges);
                free(starts);
                free(dist);
                free(prev);
                free(prev_token);
                return NULL;
            }
            memcpy(remainder, work_src + (work_len - total_uncrunched), (size_t)total_uncrunched);
            remainder[total_uncrunched] = remainder_byte;
        } else {
            remainder = (uint8_t *)malloc(1);
            if (!remainder) {
                free(token_list);
                free(transitions.edges);
                free(starts);
                free(dist);
                free(prev);
                free(prev_token);
                return NULL;
            }
            remainder[0] = remainder_byte;
        }

        for (int i = 0; i < safety; i++) {
            emit_token(&out, &out_len_local, &out_cap, work_src, &token_list[i]);
        }
        append_byte(&out, &out_len_local, &out_cap, (uint8_t)TERMINATOR);
        if (remainder_len > 1) {
            append_bytes(&out, &out_len_local, &out_cap, remainder + 1, remainder_len - 1);
        }

        uint8_t *final_out = NULL;
        int final_len = 0;
        int final_cap = 0;

        append_bytes(&final_out, &final_len, &final_cap, addr, 2);
        append_byte(&final_out, &final_len, &final_cap, (uint8_t)(*optimal_run - 1));
        append_byte(&final_out, &final_len, &final_cap, remainder[0]);
        append_bytes(&final_out, &final_len, &final_cap, out, out_len_local);

        free(out);
        free(remainder);
        out = final_out;
        out_len_local = final_len;
    } else {
        if (!opt->sfx) {
            append_byte(&out, &out_len_local, &out_cap, (uint8_t)(*optimal_run - 1));
        }
        for (int i = 0; i < token_count; i++) {
            emit_token(&out, &out_len_local, &out_cap, work_src, &token_list[i]);
        }
        append_byte(&out, &out_len_local, &out_cap, (uint8_t)TERMINATOR);
    }

    free(transitions.edges);
    free(starts);
    free(dist);
    free(prev);
    free(prev_token);
    free(token_list);

    *out_len = out_len_local;
    return out;
}

int main(int argc, char **argv) {
    Options opt;
    memset(&opt, 0, sizeof(opt));
    const char *jmp_arg = NULL;
    bool use_x2 = false;

    if (argc < 3) {
        usage();
        return 1;
    }

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "-h") == 0) {
            usage();
            return 0;
        }
    }

    for (int i = 1; i < argc - 2; i++) {
        if (strcmp(argv[i], "-q") == 0) {
            opt.quiet = true;
        } else if (strcmp(argv[i], "--selfcheck") == 0) {
            opt.selfcheck = true;
        } else if (strcmp(argv[i], "-p") == 0) {
            opt.prg = true;
        } else if (strcmp(argv[i], "-i") == 0) {
            opt.inplace = true;
            opt.prg = true;
        } else if (strcmp(argv[i], "-b") == 0) {
            opt.blank = true;
        } else if (strcmp(argv[i], "-x") == 0 || strcmp(argv[i], "-x2") == 0) {
            bool is_x2 = (strcmp(argv[i], "-x2") == 0);
            if (i + 1 >= argc) {
                usage();
                return 1;
            }
            opt.sfx = true;
            opt.sfxmode = is_x2 ? 1 : 0;
            opt.prg = true;
            if (!parse_jmp(argv[i + 1], &opt.jmp)) {
                fprintf(stderr, "Invalid jump address: %s\n", argv[i + 1]);
                return 1;
            }
            jmp_arg = argv[i + 1];
            use_x2 = is_x2;
            i++;
        }
    }

    if (opt.sfx && opt.inplace) {
        fprintf(stderr, "Can't create an sfx prg with inplace crunching\n");
        return 1;
    }

    const char *in_path = argv[argc - 2];
    const char *out_path = argv[argc - 1];

    size_t src_len = 0;
    uint8_t *src = load_file(in_path, &src_len);
    if (!src || src_len == 0) {
        fprintf(stderr, "Failed to read input file\n");
        free(src);
        return 1;
    }

    int source_len = (int)src_len;
    const uint8_t *crunch_src = src;
    int crunch_len = (int)src_len;
    uint8_t addr[2] = {0, 0};
    uint16_t decrunch_to = 0;
    uint16_t load_to = 0;

    if (opt.prg) {
        if (crunch_len < 2) {
            fprintf(stderr, "Input too small for PRG\n");
            free(src);
            return 1;
        }
        addr[0] = crunch_src[0];
        addr[1] = crunch_src[1];
        decrunch_to = (uint16_t)(addr[0] + 256 * addr[1]);
        crunch_src += 2;
        crunch_len -= 2;
    }

    int optimal_run = LONGESTRLE;
    int crunched_len = 0;
    uint8_t *crunched = crunch(crunch_src, crunch_len, &opt, addr, &crunched_len, &optimal_run);
    if (!crunched) {
        fprintf(stderr, "Crunch failed\n");
        free(src);
        return 1;
    }
    int file_len = crunched_len;

    if (opt.sfx) {
        const uint8_t *boot_src = NULL;
        int boot_len = 0;
        int gap = 0;

        if (opt.sfxmode == 0) {
            if (opt.blank) {
                boot_src = blank_boot;
                boot_len = (int)sizeof(blank_boot);
                gap = 5;
            } else {
                boot_src = boot;
                boot_len = (int)sizeof(boot);
                gap = 0;
            }
        } else {
            boot_src = boot2;
            boot_len = (int)sizeof(boot2);
            gap = 0;
        }

        uint8_t *boot_buf = (uint8_t *)malloc((size_t)boot_len);
        if (!boot_buf) {
            fprintf(stderr, "Allocation failed\n");
            free(crunched);
            free(src);
            return 1;
        }
        memcpy(boot_buf, boot_src, (size_t)boot_len);

        int sfx_file_len = boot_len + crunched_len;
        int start_address = 0x10000 - crunched_len;
        int transf_address = sfx_file_len + 0x6ff;

        if (opt.sfxmode == 0) {
            boot_buf[0x1e + gap] = (uint8_t)(transf_address & 0xff);
            boot_buf[0x1f + gap] = (uint8_t)(transf_address >> 8);

            boot_buf[0x3f + gap] = (uint8_t)(start_address & 0xff);
            boot_buf[0x40 + gap] = (uint8_t)(start_address >> 8);

            boot_buf[0x42 + gap] = (uint8_t)(decrunch_to & 0xff);
            boot_buf[0x43 + gap] = (uint8_t)(decrunch_to >> 8);

            boot_buf[0x7d + gap] = (uint8_t)(opt.jmp & 0xff);
            boot_buf[0x7e + gap] = (uint8_t)(opt.jmp >> 8);

            boot_buf[0xcc + gap] = (uint8_t)(optimal_run - 1);
        } else {
            boot_buf[0x26] = (uint8_t)(transf_address & 0xff);
            boot_buf[0x27] = (uint8_t)(transf_address >> 8);

            boot_buf[0x21] = (uint8_t)(start_address & 0xff);
            boot_buf[0x22] = (uint8_t)(start_address >> 8);

            boot_buf[0x23] = (uint8_t)(decrunch_to & 0xff);
            boot_buf[0x24] = (uint8_t)(decrunch_to >> 8);

            boot_buf[0x85] = (uint8_t)(opt.jmp & 0xff);
            boot_buf[0x86] = (uint8_t)(opt.jmp >> 8);

            boot_buf[0xd4] = (uint8_t)(optimal_run - 1);
        }

        uint8_t *final_out = (uint8_t *)malloc((size_t)(boot_len + crunched_len));
        if (!final_out) {
            fprintf(stderr, "Allocation failed\n");
            free(boot_buf);
            free(crunched);
            free(src);
            return 1;
        }
        memcpy(final_out, boot_buf, (size_t)boot_len);
        memcpy(final_out + boot_len, crunched, (size_t)crunched_len);
        free(boot_buf);
        free(crunched);
        crunched = final_out;
        crunched_len += boot_len;
        file_len = crunched_len;
        load_to = 0x0801;
    }

    uint16_t decrunch_end = (uint16_t)(decrunch_to + crunch_len - 1);

    if (opt.inplace) {
        load_to = (uint16_t)(decrunch_end - crunched_len + 1);
        uint8_t *final_out = (uint8_t *)malloc((size_t)(crunched_len + 2));
        if (!final_out) {
            fprintf(stderr, "Allocation failed\n");
            free(crunched);
            free(src);
            return 1;
        }
        final_out[0] = (uint8_t)(load_to & 0xff);
        final_out[1] = (uint8_t)(load_to >> 8);
        memcpy(final_out + 2, crunched, (size_t)crunched_len);
        free(crunched);
        crunched = final_out;
        file_len = crunched_len + 2;
    }

    if (!save_file(out_path, crunched, (size_t)file_len)) {
        fprintf(stderr, "Failed to write output file\n");
        free(crunched);
        free(src);
        return 1;
    }

    if (!opt.quiet) {
        double ratio = (double)crunched_len * 100.0 / (double)source_len;
        printf("input file  %s: %s, $%04x - $%04x : %d bytes\n",
               opt.prg ? "PRG" : "RAW", in_path, decrunch_to, decrunch_end, source_len);
        printf("output file %s: %s, $%04x - $%04x : %d bytes\n",
               (opt.sfx || opt.inplace) ? "PRG" : "RAW", out_path, load_to,
               (uint16_t)(load_to + crunched_len - 1), crunched_len);
        printf("crunched to %.2f%% of original size\n", ratio);
    }

    if (opt.selfcheck) {
        char out_go[1024];
        char flags[256];
        char cmd[2048];
        int used = 0;

        if (snprintf(out_go, sizeof(out_go), "%s.go", out_path) >= (int)sizeof(out_go)) {
            fprintf(stderr, "Selfcheck: output path too long\n");
            free(crunched);
            free(src);
            return 1;
        }

        flags[0] = '\0';
        used = snprintf(flags, sizeof(flags), "-q");
        if (used < 0 || used >= (int)sizeof(flags)) {
            fprintf(stderr, "Selfcheck: flags buffer too small\n");
            free(crunched);
            free(src);
            return 1;
        }
        if (opt.inplace) {
            strncat(flags, " -i", sizeof(flags) - strlen(flags) - 1);
        } else if (opt.prg && !opt.sfx) {
            strncat(flags, " -p", sizeof(flags) - strlen(flags) - 1);
        }
        if (opt.blank) {
            strncat(flags, " -b", sizeof(flags) - strlen(flags) - 1);
        }
        if (opt.sfx) {
            if (!jmp_arg) {
                static char jmp_buf[8];
                snprintf(jmp_buf, sizeof(jmp_buf), "$%04x", opt.jmp);
                jmp_arg = jmp_buf;
            }
            if (use_x2) {
                strncat(flags, " -x2 ", sizeof(flags) - strlen(flags) - 1);
            } else {
                strncat(flags, " -x ", sizeof(flags) - strlen(flags) - 1);
            }
            strncat(flags, jmp_arg, sizeof(flags) - strlen(flags) - 1);
        }

        snprintf(cmd, sizeof(cmd), "go run tscrunch.go %s \"%s\" \"%s\"", flags, in_path, out_go);
        if (system(cmd) != 0) {
            fprintf(stderr, "Selfcheck: go encoder failed\n");
        }

        long sz_c = file_size(out_path);
        long sz_go = file_size(out_go);
        printf("Selfcheck sizes (bytes): C=%ld Go=%ld\n", sz_c, sz_go);
    }

    free(crunched);
    free(src);
    return 0;
}
