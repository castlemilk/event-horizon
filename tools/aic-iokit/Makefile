# User-space IOKit LMAC bring-up for the AIC8800D80.
# Runs unprivileged with SIP enabled — no DriverKit, no system extension.
aic_iokit: aic_iokit.c
	clang -O2 -Wall -o aic_iokit aic_iokit.c -framework IOKit -framework CoreFoundation

clean:
	rm -f aic_iokit
