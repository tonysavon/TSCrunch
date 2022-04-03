/*

decrunch.asm

NMOS 6502 decompressor for data stored in TSCrunch format.

This code is written for the KickAssembler assembler.

Copyright Antonio Savona 2022.

*/


//#define INPLACE 		//Enables inplace decrunching. Use -i switch when crunching. 

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
			cpy #3
			bne !- 
			
			pha
			
			lda lzput
			sta optRun + 1
			
			tya
			ldy #0
			beq update_getonly
	#else 
			ldy #0			

			lda (tsget),y
			sta optRun + 1

			inc tsget
			bne entry2
			inc tsget + 1
	#endif
	
	entry2:		
			lax (tsget),y
			
			bmi rleorlz
			
			cmp #$20
			bcs lz2	

	//literal
			
	#if INPLACE
			
			inc tsget
			beq updatelit_hi
		return_from_updatelit:
		
		ts_delit_loop:

			lda (tsget),y
			sta (tsput),y
			iny
			dex
			
			bne ts_delit_loop	
			
			tya
			tax
			//carry is clear
			ldy #0
	#else	//not inplace
			tay
			
		ts_delit_loop:
			
			lda (tsget),y
			dey
			sta (tsput),y
			
			bne ts_delit_loop
			
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
	
	#if INPLACE		
	updatelit_hi:
			inc tsget+1
			bcc return_from_updatelit
	#endif		
	updateput_hi:
			inc tsput+1
			clc
			bcc putnoof
				
											
	rleorlz:
			alr #$7f
			bcc ts_delz		

		//RLE
			beq optRun
				
		plain:
			ldx #2
			iny
			sta tstemp		//number of bytes to de-rle		

			lda (tsget),y	//fetch rle byte
			ldy tstemp
		!runStart:
			sta (tsput),y
			
		ts_derle_loop:
			
			dey
			sta (tsput),y

			bne ts_derle_loop
			
			//update zero page with a = runlen, x = 2 , y = 0 
			lda tstemp		

			bcs updatezp_noclc
			
	   done:
#if INPLACE	   
	   		pla
	   		sta (tsput),y
#endif	   		
			rts	
	//LZ2	
		lz2:
			beq done
			
			ora #$80
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
		
			ldx #2
			//lz MUST decrunch forward	
	lz_put:
			sta lzput+1
			
			ldy #0
	
			lda (lzput),y
			sta (tsput),y
	
	ts_delz_loop:
	
			iny
		
			lda (lzput),y
			sta (tsput),y
			
	lzto:	cpy #0
			bne ts_delz_loop 
			
			tya
			
			//update zero page with a = runlen, x = 2, y = 0
			ldy #0
			//clc not needed as we have len - 1 in A (from the encoder) and C = 1
		#if INPLACE
			jmp updatezp_noclc
		#else	
			bcs updatezp_noclc
		#endif
		
	optRun:	
			ldy #255
			sty tstemp

			ldx #1
			//A is zero		
			bne !runStart-		

	long:
			//carry is clear and compensated for from the encoder
			adc (tsget),y
			sta lzput
			iny
			lax (tsget),y
			ora #$80
			adc tsput + 1
				
			cpx #$80
			rol lzto + 1
			ldx #3
	
			bne lz_put
	
}