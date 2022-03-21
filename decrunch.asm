/*

decrunch.asm

NMOS 6502 decompressor for data stored in TSCrunch format.

This code is written for the KickAssembler assembler.

Copyright Antonio Savona 2022.

*/

#define FASTDECRUNCHER // 1.5% faster, at the cost of 8 bytes of code. I'll have it!
//#define INPLACE //Enables inplace decrunching. Use -i switch when crunching. 

.label tsget 	= $f8	//2 bytes
.label tstemp	= $fa
.label tsput 	= $fb	//2 bytes
.label lzput 	= $fd	//2 bytes


#if INPLACE

.macro TS_DECRUNCH(src)
{
		lda #<src
		sta.zp tsget
		lda #>src
		sta.zp tsget + 1
		jsr tsdecrunch
}

#else

.macro TS_DECRUNCH(src,dst)
{
		lda #<src
		sta.zp tsget
		lda #>src
		sta.zp tsget + 1
		lda #<dst
		sta.zp tsput
		lda #>dst
		sta.zp tsput + 1
		jsr tsdecrunch
}

#endif


tsdecrunch:
{
	decrunch:

	#if INPLACE
			ldy #$ff
		!:	iny
			lda (tsget),y
			sta tsput , y	//last iteration trashes lzput, with no effect.
			cpy #2
			bne !- 
			
			pha
			tya
			ldy #0
			beq update_getonly
	#else
			ldy #0			
	#endif

	entry2:		
			lax (tsget),y
			
			bmi rleorlz
			beq done
	literal:
	#if RLEONLY
	#else
			cmp #$40
			bcs lz2	
	#endif
	
	
	#if INPLACE

			inc tsget
			bne !+
			inc tsget + 1	
		!:	lda (tsget),y
			sta (tsput),y
			iny
			dex
			bne !-	
			tya
			tax
			//carry is clear
			ldy #0
	#else
			tay
		!:
			lda (tsget),y
			dey
			sta (tsput),y
			bne !-
			
			txa
			inx
	#endif
			
	updatezp_noclc:
			adc tsput
			sta tsput
			bcs updateput_hi
		putnoof:
			txa
		update_getonly:
			adc tsget
			sta tsget
			bcc entry2
			inc tsget+1
			bcs entry2
			
	updateput_hi:
			inc tsput+1
			clc
			bcc putnoof
								
	rleorlz:
	
	#if RLEONLY
			anc #$7f
	#else	
			alr #$7f
			bcs ts_delz		
	#endif
			sta tstemp		//number of bytes to de-rle
			iny
			lda (tsget),y	//fetch rle byte
			ldy tstemp
			
	#if FASTDECRUNCHER
			dey
			sta (tsput),y
	#endif
	
	ts_derle_loop:
			dey
			sta (tsput),y
			bne ts_derle_loop

			//update zero page with a = runlen, x = 2 , y = 0 
			lda tstemp
			
			ldx #2
			//clc not needed as coming from anc
			bcc updatezp_noclc
			
	   done:
#if INPLACE	   
	   		pla
	   		sta (tsput),y
#endif	   		
			rts	
	//LZ2	
	#if RLEONLY
	#else	
		lz2:
			eor #$ff - $40
			adc tsput
			sta lzput
			lda tsput + 1
			sbc #$00
			sta lzput + 1 		
	
			//y already zero			
			lda (lzput),y
			sta (tsput),y
			iny		
			lda (lzput),y
			sta (tsput),y
					
			tya //y = a = 1. 
			tax //y = a = x = 1. a + carry = 2
			dey //ldy #0
			beq updatezp_noclc

	//LZ
	ts_delz:
			
			lsr 
			sta lzto + 1
			
			iny
			
			lda tsput
			bcc long
			
			sbc (tsget),y
			sta lzput
			lda tsput+1
	
			sbc #$00
		
			//lz MUST decrunch forward	
			sta lzput+1
	
			ldx #2
	lz_put:
			ldy #0
	#if FASTDECRUNCHER
			lda (lzput),y
			sta (tsput),y
			iny
	#endif		
			lda (lzput),y
			sta (tsput),y
		!:	iny
		
			lda (lzput),y
			sta (tsput),y
			
	lzto:	cpy #0
			bne !- 
			
			tya
			
			//update zero page with a = runlen, x = 2, y = 0
			ldy #0
			//clc not needed as we have len - 1 in A (from the encoder) and C = 1
			
			bcs updatezp_noclc
	long:
			//carry is clear and compensated for from the encoder
			adc (tsget),y
			sta lzput
			iny
			lax (tsget),y
			ora #$80
			adc tsput + 1
			sta lzput + 1		
			cpx #$80
			rol lzto + 1
			ldx #3
	
			bne lz_put

	#endif	
}