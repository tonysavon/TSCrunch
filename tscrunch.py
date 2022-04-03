#!/usr/bin/env python

"""
TSCrunch 1.3 - binary cruncher, by Antonio Savona
"""

import sys

REVERSELITERAL	=	False
VERBOSE			=	True
PRG				=	False
SFX 			=	False
INPLACE			=	False

DEBUG = False

LONGESTRLE		=	64
LONGESTLONGLZ	=	64 
LONGESTLZ 		=	32
LONGESTLITERAL	=	31
MINRLE			=	2
MINLZ			=	3
LZOFFSET		=	32767
LZ2OFFSET 		=	94

RLEMASK 		= 	0x81
LZMASK			= 	0x80
LITERALMASK 	= 	0x00
LZ2MASK 		=	0x00

TERMINATOR 		=	LONGESTLITERAL + 1 

ZERORUNID		=	4
LZ2ID 			=	3
LZID 			= 	2
RLEID 			= 	1
LITERALID 		= 	0


from scipy.sparse.csgraph import dijkstra
from scipy.sparse import csr_matrix

boot = [

	0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, 0x32, 0x30, 0x36, 0x31, 0x00,
	0x00, 0x00, 0x78, 0xA2, 0xC9, 0xBD, 0x1A, 0x08, 0x95, 0x00, 0xCA, 0xD0,
	0xF8, 0x4C, 0x02, 0x00, 0x34, 0xBD, 0x00, 0x10, 0x9D, 0x00, 0xFF, 0xE8,
	0xD0, 0xF7, 0xC6, 0x04, 0xC6, 0x07, 0xA5, 0x04, 0xC9, 0x07, 0xB0, 0xED,
	0xA0, 0x00, 0xB3, 0x21, 0x30, 0x21, 0xC9, 0x20, 0xB0, 0x3F, 0xA8, 0xB9,
	0xFF, 0xFF, 0x88, 0x99, 0xFF, 0xFF, 0xD0, 0xF7, 0x8A, 0xE8, 0x65, 0x25,
	0x85, 0x25, 0xB0, 0x77, 0x8A, 0x65, 0x21, 0x85, 0x21, 0x90, 0xDF, 0xE6,
	0x22, 0xB0, 0xDB, 0x4B, 0x7F, 0x90, 0x3A, 0xF0, 0x6B, 0xA2, 0x02, 0x85,
	0x53, 0xC8, 0xB1, 0x21, 0xA4, 0x53, 0x91, 0x25, 0x88, 0x91, 0x25, 0xD0,
	0xFB, 0xA9, 0x00, 0xB0, 0xD5, 0xA9, 0x37, 0x85, 0x01, 0x58, 0x4C, 0x5B,
	0x00, 0xF0, 0xF6, 0x09, 0x80, 0x65, 0x25, 0x85, 0x9B, 0xA5, 0x26, 0xE9,
	0x00, 0x85, 0x9C, 0xB1, 0x9B, 0x91, 0x25, 0xC8, 0xB1, 0x9B, 0x91, 0x25,
	0x98, 0xAA, 0x88, 0xF0, 0xB1, 0x4A, 0x85, 0xA0, 0xC8, 0xA5, 0x25, 0x90,
	0x33, 0xF1, 0x21, 0x85, 0x9B, 0xA5, 0x26, 0xE9, 0x00, 0x85, 0x9C, 0xA2,
	0x02, 0xA0, 0x00, 0xB1, 0x9B, 0x91, 0x25, 0xC8, 0xB1, 0x9B, 0x91, 0x25,
	0xC8, 0xB9, 0x9B, 0x00, 0x91, 0x25, 0xC0, 0x00, 0xD0, 0xF6, 0x98, 0xA0,
	0x00, 0xB0, 0x83, 0xE6, 0x26, 0x18, 0x90, 0x84, 0xA0, 0xFF, 0x84, 0x53,
	0xA2, 0x01, 0xD0, 0x96, 0x71, 0x21, 0x85, 0x9B, 0xC8, 0xB3, 0x21, 0x09,
	0x80, 0x65, 0x26, 0x85, 0x9C, 0xE0, 0x80, 0x26, 0xA0, 0xA2, 0x03, 0xD0,
	0xC4

		]

def load_raw(fi):
	data = bytes(fi.read())
	return data

def save_raw(fo, data):
	fo.write(bytes(data))
	
#finds all the occurrences of prefix in the range [max(0,i - LZOFFSET),i) 	
#the search window is quite small, so brute force here performs as well as suffix trees
def findall(data,prefix,i,minlz = MINLZ):
	x0 = max(0,i - LZOFFSET)
	x1 = min(i + minlz - 1, len(data))
	f = 1
	while f >= 0:
		f = data.rfind(prefix,x0,x1)
		if f >= 0:
			yield f
			x1 = f + minlz - 1
	
#pretty prints a progress bar	
def progress(description,current,total):
	percentage = 100 *current // total
	tchars = 16 * current // total
	sys.stdout.write("\r%s [%s%s]%02d%%" %(description,'*'*tchars, ' '*(16-tchars), percentage))
	
	
def findOptimalZero(src):
	zeroruns = dict()
	i = 0
	while i < len(src) - 1:
		
		if src[i] == 0:
			j = i + 1
			while j < len(src) and src[j] == 0 and j-i < 256:
				j+=1
			if j - i >= MINRLE:
				zeroruns[j-i] = zeroruns.get(j-i,0) + 1	
			i = j
		else:
			i+=1
	
	if len(zeroruns) > 0:
		return 	min(list(zeroruns.items()),key = lambda x:-x[0]*(x[1]**1.1))[0]
	else: 
		return LONGESTRLE	
	
class Token:
	def __init__(self,src = None):
		self.type = None


class ZERORUN(Token):
	def __init__(self,src,i,size = LONGESTRLE, token = None):
		self.type = ZERORUNID
		self.size = size
		if token != None:
			self.fromToken(token)
		else:
			if not(i+size < len(src) and src[i:i+size] == bytes([0] * size)):
				self.size = 0
			
	def getCost(self):
		return 1
	
	def getPayload(self):
		return [RLEMASK]
	
class RLE(Token):
	def __init__(self,src,i,size = None, token = None):
		self.type = RLEID
		self.rleByte = src[i]
		
		if token != None:
			self.fromToken(token)
		
		elif size == None:
			x = 0
			while i + x < len(src) and x < LONGESTRLE and src[i + x] == src[i]:
				x+=1
			self.size = x
		else:
			self.size = size
	
	def getCost(self):
		return 2 + 0.00128 - 0.00001 * self.size

	def getPayload(self):
		return [RLEMASK | (((self.size-1) << 1) & 0x7f ), self.rleByte]
	
	
class LZ(Token):
	def __init__(self,src,i, size = None, offset = None, minlz = MINLZ, token = None):
		self.type = LZID
	
		if token != None:
			self.fromToken(token)
			
		elif size == None: 
			
			bestpos , bestlen = i - 1 , 0
	
			if len(src) - i >= minlz:
				for j in findall(src,src[i:i+minlz],i,minlz):
					
					l = minlz 
					while i + l < len(src) and l < LONGESTLONGLZ and src[j + l] == src[i + l] :
						l+=1
					if l > bestlen:
						bestpos, bestlen = j , l
	
			self.size = bestlen
			self.offset = i - bestpos	
			
		else:
			self.size = size
		if offset != None:
			self.offset = offset
			
	def getCost(self):
		return (2 if (self.offset < 256) and (self.size <= LONGESTLZ) else 3) + 0.00134 - 0.00001 * self.size
		
	def getPayload(self):
		if self.offset >= 256 or self.size > LONGESTLZ:
			negoffset = (0-self.offset) 
			return [LZMASK | ((((self.size - 1)>>1)<< 2) & 0x7f) | 0 , (negoffset & 0xff) , ((negoffset >> 8) & 0x7f) | (((self.size - 1) & 1) << 7 )]	
		else:
			return [LZMASK | (((self.size - 1)<< 2) & 0x7f) | 2 , (self.offset & 0xff) ] 


class LZ2(Token):
	def __init__(self,src,i, offset = None, token = None):
		self.type = LZ2ID
		self.size = 2
		
		if token != None:
			self.fromToken(token)
			
		elif offset == None: 
			if i+2 < len(src):
				o = src.rfind(src[i:i+2], max(0,i-LZ2OFFSET),i + 1)
				if o >= 0:
					self.offset = i - o
				else:
					self.offset = -1
			
			else:
				 self.offset = - 1
			
		else:
			self.offset = offset
		

	def getCost(self):
		return 1 + 0.00132 - 0.00001 * self.size
		
	def getPayload(self):
		return [LZ2MASK | (127 - self.offset) ]
	
	
class LIT(Token):
	def __init__(self,src,i, token = None):
		self.type = LITERALID	
		self.size = 1
		self.start = i

		if token != None:
			self.fromToken(token)

	def getCost(self):
		return self.size + 1 + 0.00130 - 0.00001 * self.size

	def getPayload(self):
		return bytes([LITERALMASK | (self.size)]) + src[self.start : self.start + self.size]
	
	
class Cruncher:

	def __init__(self, src = None):
		self.crunched = []
		self.token_list = []
		self.src = src
		self.graph = dict()
		self.crunchedSize = 0

	def get_path(self,p):	
		i = len(p) - 1
		path = [i]
		while p[i] >= 0:
			path.append(p[i])
			i = p[i]
		path.reverse()
	
		return list(zip(path[::],path[1::]))
	
	def prepend(self, data):
		self.crunched = bytes(data) + bytes(self.crunched)
	
	def ocrunch(self):
		starts = set()
		ends = set()

		if INPLACE:	
			remainder = self.src[-1:]
			src = bytes(self.src[:-1])
		else:
			src = bytes(self.src)
		
		
		self.optimalRun = findOptimalZero(src)
		
		progress_string = "Populating LZ layer\t"
		
		for i in range(0,len(src)):	
			if VERBOSE and ((i & 255) == 0):
				progress(progress_string,i,len(src))
			lz2 = None
			rle = RLE(src,i)
			
			#don't compute prefix for same bytes or this will explode
			#start computing for prefixes larger than RLE
			if rle.size < LONGESTLONGLZ - 1:	
				lz = LZ(src,i, minlz = rle.size + 1)
			else:
				lz = LZ(src,i,size = 1) #start with a dummy LZ

			if lz.size >= MINLZ or rle.size >= MINRLE:
				starts.add(i)
			while lz.size >= MINLZ and lz.size > rle.size:
				ends.add(i+lz.size)
				self.graph[(i,i+lz.size)] = lz
				lz = LZ(src, i, size = lz.size - 1, offset = lz.offset)
			while rle.size >= MINRLE:
				ends.add(i+rle.size)
				self.graph[(i,i+rle.size)] = rle
				rle = RLE(src, i, rle.size - 1)
	
			lz2 = LZ2(src,i)
			if lz2.offset > 0:
				self.graph[(i,i+2)] = lz2
				starts.add(i)
				ends.add(i + 2)
				 
			zero = ZERORUN(src,i,self.optimalRun)
			if zero.size > 0:
				self.graph[(i,i+self.optimalRun)] = zero
				starts.add(i)
				ends.add(i+self.optimalRun)
				
				
		if VERBOSE:
			progress(progress_string,1,1)
			sys.stdout.write('\n')
			
		starts.add(len(src))
		starts = sorted(list(starts))
		ends = [0] + sorted(list(ends))	

		progress_string = "Closing gaps\t\t"

		e,s = 0,0
		while e < len(ends) and s < len(starts):
			if VERBOSE and ((s & 255) == 0):
				progress(progress_string,s,len(starts))
			end = ends[e]
			if end < starts[s]:
				#bridge		
				while starts[s] - end >= LONGESTLITERAL:
					key = (end,end + LONGESTLITERAL)
					if not key in self.graph:
						lit = LIT(src,end)
						lit.size = LONGESTLITERAL
						self.graph[key] = lit
					end+=LONGESTLITERAL
				s0 = s
				while s0 < len(starts) and starts[s0] - end < LONGESTLITERAL:
					key = (end,starts[s0])
					if not key in self.graph:
						lit = LIT(src,end)
						lit.size = starts[s0] - end
						self.graph[key] = lit
					s0+=1
				e+=1
			else:
				s+=1
	
		if VERBOSE:
			progress(progress_string,1,1)
			sys.stdout.write('\n')
	
		progress_string = "Populating graph\t"
		
		if VERBOSE:
			progress(progress_string,0,3)
		weights = tuple(v.getCost() for v in self.graph.values())
		if VERBOSE:
			progress(progress_string,1,3)
		sources = tuple(s for s, _ in self.graph.keys())
		if VERBOSE:
			progress(progress_string,2,3)
		targets = tuple(t for _, t in self.graph.keys())
		n = len(src) + 1
		dgraph = csr_matrix((weights, (sources, targets)), shape=(n, n))
		if VERBOSE:
			progress(progress_string,1,1)
			sys.stdout.write('\ncomputing shortest path\n')		
		d,p = dijkstra(dgraph,indices = 0,return_predecessors = True)
		for key in self.get_path(p):
			self.token_list.append(self.graph[key])

		if INPLACE:
			safety = len(self.token_list)
			segment_uncrunched_size = 0
			segment_crunched_size = 0
			total_uncrunched_size = 0
			for i in range(len(self.token_list)-1,-1,-1):
				segment_crunched_size+=len(self.token_list[i].getPayload()) #token size
				segment_uncrunched_size+=self.token_list[i].size #decrunched token raw size
				if segment_uncrunched_size <= segment_crunched_size + 0:
					safety = i
					total_uncrunched_size+=segment_uncrunched_size
					segment_uncrunched_size = 0
					segment_crunched_size = 0

			for token in (self.token_list[:safety]):
				self.crunched.extend(token.getPayload())
			if total_uncrunched_size > 0:
				remainder = src[-total_uncrunched_size:] + remainder
			self.crunched.extend(bytes([TERMINATOR]) + remainder[1:])
			self.crunched = addr + bytes([self.optimalRun - 1]) + remainder[:1] + bytes(self.crunched)
			
		else:
			if not SFX:
				self.crunched.extend([self.optimalRun - 1])
			for token in (self.token_list):
				self.crunched.extend(token.getPayload())	
			self.crunched = bytes(self.crunched + [TERMINATOR])
		self.crunchedSize = len(self.crunched)	

		if DEBUG:
			nlz2 = 0; nlzl = 0; nlz = 0; nrle = 0; nlit = 0; nz = 0; nlit1 = 0

			for token in self.token_list:
				if token.type == LITERALID:
					nlit+=1
					if token.size == 1:
						nlit1+=1
				elif token.type == LZ2ID:
					nlz2+=1
				elif token.type == RLEID:
					nrle +=1
				elif token.type == ZERORUNID:
					nz +=1
				else:
					if len(token.getPayload()) == 3:
						nlzl+=1
					else:
						nlz+=1
			
			tot = sum((nlz,nlzl,nlz2,nrle,nz,nlit))
			sys.stdout.write ("lz: %d, lzl: %d, lz2: %d, rle: %d, nz: %d, lit: %d (1 = %d) tot: %d\n" % (nlz,nlzl,nlz2,nrle,nz,nlit,nlit1,tot))
	

class Decruncher:
	def __init__(self, src = None):

		self.src = src
		self.decrunch()
				
	def decrunch(self, src = None):
		
		if src != None:
			self.src = src
		if self.src == None:
			self.decrunched = None
		else:
			
			nlz2 = 0; nlz = 0; nrle = 0; nz = 0; nlit = 0; 
			
			self.decrunched = bytearray([])
			self.optimalRun = self.src[0] + 1
			i=1
			while self.src[i] != TERMINATOR:
				
				code = self.src[i]
				if ((code & 0x80 == LITERALMASK) and code & 0x7f < 32) :
										
					run = (code & 0x1f)
					chunk = self.src[i + 1 : i + run + 1]
					if REVERSELITERAL:
						chunk.reverse()
					self.decrunched.extend(chunk)
					i+=run + 1
					nlit+=1
							
				elif (code & 0x80 == LZ2MASK):
					
					run = 2
					offset =  127 - (code & 0x7f) 
					p = len(self.decrunched)
					for l in range(run):
						self.decrunched.append(self.decrunched[p-offset + l])
					i+=1
					nlz2+=1	
					
				elif (code & 0x81) == RLEMASK and (code & 0x7e) != 0:
					run = ((code & 0x7f) >> 1) + 1
					self.decrunched.extend([self.src[i+1]] * run)
					i+=2
					nrle+=1
					
				elif (code & 0x81) == RLEMASK and (code & 0x7e)	== 0:
					run = self.optimalRun
					self.decrunched.extend(bytes([0] * run))
					i+=1
					nz+=1
					
				else:
					if (code & 2) == 2:
						run = ((code & 0x7f) >> 2) + 1
						offset = self.src[i+1]
						i+=2
					else:
						lookahead = self.src[i+2]
						run = 1 + (((code & 0x7f) >> 2) << 1) + (1 if (lookahead & 128 == 128) else 0)
						offset =  32768 - (self.src[i+1]  + 256 * (lookahead & 0x7f))
						i+=3
					p = len(self.decrunched)
					for l in range(run):
						self.decrunched.append(self.decrunched[p-offset + l])			
					nlz+=1
					
			tot = sum((nlz,nlz2,nrle,nz,nlit))
			sys.stdout.write ("lz: %d, lz2: %d, rle: %d, nz: %d,  lit: %d tot: %d\n" % (nlz,nlz2,nrle,nz,nlit,tot))
	
def usage():
	print ("TSCrunch 1.3 - binary cruncher, by Antonio Savona")
	print ("Usage: tscrunch [-p] [-i] [-r] [-q] [-x] infile outfile")
	print (" -p  : input file is a prg, first 2 bytes are discarded")
	print (" -x  $addr: creates a self extracting file (forces -p)")
	print (" -i  : inplace crunching (forces -p)")
	print (" -q  : quiet mode")
	

if __name__ == "__main__":

	if "-h" in sys.argv or len(sys.argv) < 3:
		usage()
	else:
	
		if "-q" in sys.argv:
			VERBOSE = False

		if "-x" in sys.argv:
			SFX = True
			PRG = True
			jmp_str = sys.argv[sys.argv.index("-x") + 1].strip("$")
			jmp = int(jmp_str,base = 16)
		
		if "-i" in sys.argv:
			INPLACE = True
			PRG = True
			
		if "-p" in sys.argv:
			PRG = True
		
		if SFX and INPLACE:
			sys.stderr.write ("Can't create an sfx prg with inplace crunching\n")
			exit(-1)
			
		fr = open(sys.argv[-2],"rb")
		src = load_raw(fr)

		sourceLen = len(src)
		
		decrunchTo = 0
		loadTo = 0
		
		if PRG:
			addr = src[:2]
			src = src[2:]		
			decrunchTo = addr[0] + 256 * addr[1]

		cruncher = Cruncher(src)
		cruncher.ocrunch()
		
		if SFX:
			
			fileLen = len(boot) + len(cruncher.crunched)
			startAddress = 0x10000 - len(cruncher.crunched)
			transfAddress =  fileLen + 0x6ff
		
			boot[0x1e] = transfAddress & 0xff #transfer from
			boot[0x1f] = transfAddress >> 8
			
			boot[0x3c] = startAddress & 0xff # Depack from..
			boot[0x3d] = startAddress >> 8  
		    
			boot[0x40] = decrunchTo & 0xff # decrunch to..
			boot[0x41] = decrunchTo >> 8 
		    
			boot[0x77] = jmp & 0xff; # Jump to..
			boot[0x78] = jmp >> 8;   
			
			boot[0xc9] = cruncher.optimalRun - 1
			
			cruncher.prepend(boot)

			cruncher.crunchedSize+=len(boot)
			loadTo = 0x0801
			
		decrunchEnd = decrunchTo + len(src) - 1
		
		if INPLACE:
			loadTo = decrunchEnd - len(cruncher.crunched) + 1
			cruncher.prepend([loadTo & 255, loadTo >> 8])
			
		fo = open(sys.argv[-1],"wb")

		save_raw(fo,cruncher.crunched)
		fo.close()
		
		if VERBOSE:
			ratio = (float(cruncher.crunchedSize) * 100.0 / sourceLen)
			print ("input file  %s: %s, $%04x - $%04x : %d bytes" 
			  %("PRG" if PRG else "RAW", sys.argv[-2], decrunchTo, decrunchEnd, sourceLen))
			print ("output file %s: %s, $%04x - $%04x : %d bytes" 
			  %("PRG" if SFX or INPLACE else "RAW", sys.argv[-1],  loadTo, cruncher.crunchedSize + loadTo - 1, cruncher.crunchedSize))
			print ("crunched to %.2f%% of original size" %ratio)
			
		if DEBUG and not (SFX or INPLACE):
			decruncher = Decruncher(cruncher.crunched)
		
			fo = open("test.raw","wb")

			save_raw(fo,decruncher.decrunched)
			fo.close()
		
			assert(decruncher.decrunched == src)
