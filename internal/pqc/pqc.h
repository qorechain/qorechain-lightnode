#ifndef QORE_PQC_H
#define QORE_PQC_H

#include <stdint.h>
#include <stddef.h>

/* Dilithium-5 (ML-DSA-87) key generation */
int qore_dilithium_keygen(uint8_t *pk, uint8_t *sk);

/* Dilithium-5 sign */
int qore_dilithium_sign(uint8_t *sig, size_t *siglen,
                        const uint8_t *msg, size_t msglen,
                        const uint8_t *sk);

/* Dilithium-5 verify */
int qore_dilithium_verify(const uint8_t *sig, size_t siglen,
                          const uint8_t *msg, size_t msglen,
                          const uint8_t *pk);

#endif
