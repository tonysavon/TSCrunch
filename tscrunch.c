#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <limits.h>

/*
 * Simplified C version of TSCrunch (based on tscrunch.go)
 * This is a straightforward port with no threading or prefix optimisations.
 */

#define LONGESTRLE      64
#define LONGESTLONGLZ   64
#define LONGESTLZ       32
#define LONGESTLITERAL  31
#define MINRLE          2
#define MINLZ           3
#define LZOFFSET        256
#define LONGLZOFFSET    32767
#define LZ2OFFSET       94

#define RLEMASK         0x81
#define LZMASK          0x80
#define LITERALMASK     0x00
#define LZ2MASK         0x00
#define TERMINATOR      (LONGESTLITERAL + 1)

#define LZ2ID           3
#define LZID            2
#define RLEID           1
#define LITERALID       4
#define LONGLZID        5
#define ZERORUNID       6

static int min_int(int a,int b){return a<b?a:b;}
static int max_int(int a,int b){return a>b?a:b;}

typedef struct { int dest; long long weight; } Arc;
typedef struct {
    Arc *arcs; int count; int cap;
} Vertex;
typedef struct {
    Vertex *v; int n;
} Graph;

static Graph *graph_new(int n){
    Graph *g = (Graph*)calloc(1,sizeof(Graph));
    g->n = n;
    g->v = (Vertex*)calloc(n,sizeof(Vertex));
    return g;
}
static void graph_add_arc(Graph *g,int u,int v,long long w){
    Vertex *vert=&g->v[u];
    if(vert->count==vert->cap){
        vert->cap=vert->cap?vert->cap*2:4;
        vert->arcs=(Arc*)realloc(vert->arcs,vert->cap*sizeof(Arc));
    }
    vert->arcs[vert->count].dest=v;
    vert->arcs[vert->count].weight=w;
    vert->count++;
}

/* Simple min-heap for Dijkstra */
typedef struct {int v; long long d;} HeapItem;
static void heap_push(HeapItem *h,int *sz,HeapItem it){
    int i=(*sz)++; h[i]=it; while(i>0){int p=(i-1)/2;if(h[p].d<=h[i].d)break;HeapItem tmp=h[p];h[p]=h[i];h[i]=tmp;i=p;} }
static HeapItem heap_pop(HeapItem *h,int *sz){HeapItem r=h[0];h[0]=h[--(*sz)];int i=0;while(1){int l=2*i+1,rn=l+1,sm=i;if(l<*sz&&h[l].d<h[sm].d)sm=l;if(rn<*sz&&h[rn].d<h[sm].d)sm=rn;if(sm==i)break;HeapItem t=h[i];h[i]=h[sm];h[sm]=t;i=sm;}return r;}

static int *dijkstra(Graph *g,int src,int target){
    int n=g->n; long long *dist=(long long*)malloc(n*sizeof(long long));
    int *prev=(int*)malloc(n*sizeof(int));
    for(int i=0;i<n;i++){dist[i]=LLONG_MAX;prev[i]=-1;}
    dist[src]=0;
    HeapItem *heap=(HeapItem*)malloc(n*sizeof(HeapItem));
    int hsz=0; heap_push(heap,&hsz,(HeapItem){src,0});
    int found=0;
    while(hsz){
        HeapItem it=heap_pop(heap,&hsz); int u=it.v; if(u==target){found=1;break;}
        if(it.d!=dist[u])continue;
        for(int i=0;i<g->v[u].count;i++){
            Arc *a=&g->v[u].arcs[i]; long long alt=dist[u]+a->weight; if(alt<dist[a->dest]){dist[a->dest]=alt;prev[a->dest]=u;heap_push(heap,&hsz,(HeapItem){a->dest,alt});}
        }
    }
    free(heap);
    if(!found){free(dist);free(prev);return NULL;}
    int *path=(int*)malloc((n)*sizeof(int));int idx=0;for(int u=target;u!=-1;u=prev[u])path[idx++]=u;for(int i=0;i<idx/2;i++){int t=path[i];path[i]=path[idx-1-i];path[idx-1-i]=t;}path[idx]= -1; // sentinel
    free(dist);free(prev);return path;
}

typedef struct {int n0,n1;} Edge;

typedef struct {
    uint8_t tokentype; int size; uint8_t rlebyte; int offset; int i;
} Token;

/* token cost as in Go version */
static long long token_cost(int n0,int n1,uint8_t t){
    long long size=n1-n0; long long mdiv=LONGESTLITERAL*(1<<16);
    switch(t){
        case LZID: return mdiv*2 + 134 - size;
        case LONGLZID: return mdiv*3 + 138 - size;
        case RLEID: return mdiv*2 + 128 - size;
        case ZERORUNID: return mdiv*1;
        case LZ2ID: return mdiv*1 + 132 - size;
        case LITERALID: return mdiv*(size+1) + 130 - size;
    }
    return 0;
}

static void token_payload(uint8_t *dst,int *dstlen,const uint8_t *src,Token t){
    int n0=t.i; int p=*dstlen;
    switch(t.tokentype){
        case LZID:
            dst[p++]=LZMASK | ((((t.size - 1) << 2) & 0x7f)) | 2;
            dst[p++]=t.offset & 0xff; break;
        case LONGLZID:{
            int negoffset=-t.offset;
            dst[p++]=LZMASK | ((((t.size - 1) >> 1) << 2) & 0x7f);
            dst[p++]=negoffset & 0xff;
            dst[p++]=((negoffset>>8)&0x7f) | (((t.size-1)&1)<<7);
            break;}
        case RLEID:
            dst[p++]=RLEMASK | (((t.size-1)<<1)&0x7f);
            dst[p++]=t.rlebyte; break;
        case ZERORUNID:
            dst[p++]=RLEMASK; break;
        case LZ2ID:
            dst[p++]=LZ2MASK | (0x7f - t.offset); break;
        case LITERALID:
        default:
            dst[p++]=LITERALMASK | t.size;
            memcpy(dst+p,src+n0,t.size); p+=t.size; break;
    }
    *dstlen=p;
}

/* Compression helpers */
static Token LZ_token(const uint8_t *src,int len,int i,int size,int offset,int minlz){
    Token lz={.tokentype=LZID,.i=i};
    if(i>=0){
        int bestpos=i-1,bestlen=0;
        if(i+minlz<=len){
            int start=max_int(0,i-LONGLZOFFSET);
            for(int pos=i-1;pos>=start;pos--){
                if(pos+minlz>len) continue;
                if(memcmp(&src[pos],&src[i],minlz)==0){
                    int l=minlz;
                    while(i+l<len && l<LONGESTLONGLZ && src[pos+l]==src[i+l])l++;
                    if((l>bestlen && (i-pos < LZOFFSET || i-bestpos>=LZOFFSET || l>LONGESTLZ)) || (l>bestlen+1)){
                        bestpos=pos; bestlen=l;
                    }
                }
            }
        }
        lz.size=bestlen; lz.offset=i-bestpos;
    }else{
        lz.size=size; lz.offset=offset;
    }
    if(lz.size>LONGESTLZ || lz.offset>=LZOFFSET) lz.tokentype=LONGLZID;
    return lz;
}

static Token RLE_token(const uint8_t *src,int len,int i,int size,uint8_t rlebyte){
    Token rle={.tokentype=RLEID,.i=i};
    if(i>=0){
        rle.rlebyte=src[i]; int x=0; while(i+x<len && x<LONGESTRLE+1 && src[i+x]==src[i]) x++; rle.size=x;
    }else{ rle.size=size; rle.rlebyte=rlebyte; }
    return rle;
}

static Token ZERORUN_token(const uint8_t *src,int len,int i,int optimalRun){
    Token z={.tokentype=ZERORUNID,.i=i,.rlebyte=0,.size=0};
    if(i>=0){ int x=0; for(;x<optimalRun && i+x<len && src[i+x]==0;x++); if(x==optimalRun) z.size=optimalRun; }
    return z; }

static Token LZ2_token(const uint8_t *src,int len,int i,int size,int offset){
    Token lz2={.tokentype=LZ2ID,.offset=-1,.size=-1,.i=i};
    if(i>=0){ if(i+2<len){ int start=max_int(0,i-LZ2OFFSET); for(int pos=i-1;pos>=start;pos--){ if(src[pos]==src[i] && src[pos+1]==src[i+1]){ lz2.offset=i-pos; lz2.size=2; break; } } } }
    else{ lz2.size=size; lz2.offset=offset; }
    return lz2; }

static Token LIT_token(int i,int size){ Token t={.tokentype=LITERALID,.i=i,.size=size}; return t; }

/* crunch function, stripped down version (no SFX/inplace support) */
static uint8_t *crunch(const uint8_t *input,int len,int *outlen){
    Graph *g=graph_new(len+1);
    for(int i=0;i<len;i++) graph_add_arc(g,i,i+1,token_cost(i,i+1,LITERALID));
    // token map
    Token *token_map=(Token*)calloc((len+1)*(LONGESTLONGLZ+2),sizeof(Token)); // naive big array
    // Build tokens
    for(int i=0;i<len;i++){
        Token rle=RLE_token(input,len,i,0,0); int rlesize=min_int(rle.size,LONGESTRLE);
        Token lz=LZ_token(input,len,i,0,0,max_int(rlesize+1,MINLZ));
        Token lz2=LZ2_token(input,len,i,0,0);
        Token zero=ZERORUN_token(input,len,i,MINRLE); // simplified use MINRLE
        for(int s=lz.size;s>=MINLZ && s>rlesize;s--){ Token tmp=LZ_token(input,len,-1,s,lz.offset,MINLZ); token_map[i*(LONGESTLONGLZ+2)+s]=tmp; graph_add_arc(g,i,i+s,token_cost(i,i+s,tmp.tokentype)); }
        if(rle.size>LONGESTRLE){ Token tmp=RLE_token(input,len,-1,LONGESTRLE,input[i]); token_map[i*(LONGESTLONGLZ+2)+LONGESTRLE]=tmp; graph_add_arc(g,i,i+LONGESTRLE,token_cost(i,i+LONGESTRLE,tmp.tokentype)); }
        else{ for(int s=rle.size;s>=MINRLE;s--){ Token tmp=RLE_token(input,len,-1,s,input[i]); token_map[i*(LONGESTLONGLZ+2)+s]=tmp; graph_add_arc(g,i,i+s,token_cost(i,i+s,tmp.tokentype)); } }
        if(lz2.size==2){ token_map[i*(LONGESTLONGLZ+2)+1]=lz2; graph_add_arc(g,i,i+2,token_cost(i,i+2,lz2.tokentype)); }
        if(zero.size){ token_map[i*(LONGESTLONGLZ+2)+0]=zero; graph_add_arc(g,i,i+MINRLE,token_cost(i,i+MINRLE,zero.tokentype)); }
    }
    // Fill gaps with literals
    for(int i=0;i<len;i++){ for(int s=1;s<min_int(LONGESTLITERAL+1,len+1-i);s++){ Edge e={i,i+s}; graph_add_arc(g,e.n0,e.n1,token_cost(e.n0,e.n1,LITERALID)); Token t=LIT_token(i,s); token_map[i*(LONGESTLONGLZ+2)+s+200]=t; } }
    int *path=dijkstra(g,0,len); if(!path){fprintf(stderr,"no path\n");exit(1);} // sentinel -1
    // Build output
    uint8_t *out=(uint8_t*)malloc(len*3+100); int olen=0; // oversize
    int idx=0; while(path[idx+1]!=-1){ int n0=path[idx]; int n1=path[idx+1]; int diff=n1-n0; Token t;
        // choose token
        t=token_map[n0*(LONGESTLONGLZ+2)+diff]; if(t.tokentype==0) t=LIT_token(n0,diff); t.i=n0; token_payload(out,&olen,input,t); idx++; }
    out[olen++]=TERMINATOR; *outlen=olen; free(path); // free graph
    for(int i=0;i<g->n;i++) {
        free(g->v[i].arcs);
    }
    free(g->v);
    free(g);
    free(token_map);
    return out;
}

int main(int argc,char **argv){
    if(argc<3){fprintf(stderr,"usage: %s infile outfile\n",argv[0]);return 1;}
    FILE *fi=fopen(argv[1],"rb");
    if(!fi){
        perror("open");
        return 1;
    }
    fseek(fi,0,SEEK_END);
    long l=ftell(fi);
    fseek(fi,0,SEEK_SET);
    uint8_t *buf=malloc(l);
    size_t read_bytes=fread(buf,1,l,fi);
    if(read_bytes!=(size_t)l){
        perror("fread");
        fclose(fi);
        free(buf);
        return 1;
    }
    fclose(fi);
    int outlen; uint8_t *out=crunch(buf,l,&outlen);
    FILE *fo=fopen(argv[2],"wb"); fwrite(out,1,outlen,fo); fclose(fo);
    free(buf); free(out);
    return 0;
}

