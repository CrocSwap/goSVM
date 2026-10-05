/* Memory layout matches platform-tools v1.51's sbpfv3-solana-solana target. */
SECTIONS {
  .text 0x000000000 : { KEEP(*(.text.abort)) *(.text*) } :text
  .rodata 0x100000000 : {
    *(.rodata*) *(.data.rel.ro*) BYTE(0); . = ALIGN(8);
  } :rodata
  .bss.stack 0x200000000 (NOLOAD) : { . += 0x1000; . = ALIGN(8); } :stack
  .bss.heap 0x300000000 (NOLOAD) : { . += 0x1000; . = ALIGN(8); } :heap
  /DISCARD/ : {
    *(.comment*) *(.eh_frame*) *(*hash*) *(.bss*) *(.data*)
    *(.rel.dyn*) *(.dynamic) *(.dynsym) *(.dynstr)
  }
}
PHDRS {
  text PT_LOAD FLAGS(1);
  rodata PT_LOAD FLAGS(4);
  stack PT_LOAD FLAGS(6);
  heap PT_LOAD FLAGS(6);
}
