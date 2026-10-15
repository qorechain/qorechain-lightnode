package chaintx

import (
	"encoding/hex"
	"fmt"

	qpqc "github.com/qorechain/qorechain-pqc/go"
)

// Type URLs, from qorechain-core/proto (lightnode, pqc) and the Cosmos SDK.
const (
	TypeRegisterLightNode = "/qorechain.lightnode.v1.MsgRegisterLightNode"
	TypeHeartbeat         = "/qorechain.lightnode.v1.MsgHeartbeat"
	TypeClaimRewards      = "/qorechain.lightnode.v1.MsgClaimLightNodeRewards"
	TypeRegisterPQCKeyV2  = "/qorechain.pqc.v1.MsgRegisterPQCKeyV2"
	TypeDelegate          = "/cosmos.staking.v1beta1.MsgDelegate"
	typePQCHybridSig      = "/qorechain.pqc.v1.PQCHybridSignature"
	typeSecp256k1PubKey   = "/cosmos.crypto.secp256k1.PubKey"
)

// algorithmDilithium5 is the chain's AlgorithmID for Dilithium-5 (ML-DSA-87).
const algorithmDilithium5 = 1

// MsgRegisterLightNode registers the operator's light node.
func MsgRegisterLightNode(operator, nodeType, version string) Any {
	var b []byte
	b = putString(b, 1, operator)
	b = putString(b, 2, nodeType)
	b = putString(b, 3, version)
	return Any{TypeURL: TypeRegisterLightNode, Value: b}
}

// MsgHeartbeat is the liveness beat a registered node sends every interval.
func MsgHeartbeat(operator string) Any {
	return Any{TypeURL: TypeHeartbeat, Value: putString(nil, 1, operator)}
}

// MsgClaimRewards moves the node's accrued light-node rewards to the operator.
func MsgClaimRewards(operator string) Any {
	return Any{TypeURL: TypeClaimRewards, Value: putString(nil, 1, operator)}
}

// MsgDelegate bonds stake from the operator to a validator. The chain counts
// the operator's total bonded stake, across any bonded validators, against
// min_delegated_stake.
func MsgDelegate(delegator, validator string, amount Coin) Any {
	var b []byte
	b = putString(b, 1, delegator)
	b = putString(b, 2, validator)
	b = putMessage(b, 3, amount.marshal())
	return Any{TypeURL: TypeDelegate, Value: b}
}

// MsgRegisterPQCKeyV2 attaches a Dilithium-5 key to the account as a hybrid
// key. It is the one message the chain accepts classically signed from an
// account that has no PQC key yet, and the transaction must carry nothing else.
//
// withProof adds proof_of_possession (field 6), the key's signature over
// "qorechain-pqc-pop-v1|<chainID>|1|<sender>|<hex pubkey>". The field exists
// from chain v3.2.3; a chain before that refuses the transaction for carrying
// an unknown field, so the caller decides from the network's applied upgrades.
func MsgRegisterPQCKeyV2(chainID, sender string, pqcPub, pqcPriv []byte, withProof bool) (Any, error) {
	var b []byte
	b = putString(b, 1, sender)
	b = putBytes(b, 2, pqcPub)
	b = putUvarint(b, 3, algorithmDilithium5)
	b = putString(b, 5, "hybrid")
	if withProof {
		pop, err := qpqc.MLDSA87.Sign(pqcPriv, []byte(ProofOfPossessionMessage(chainID, sender, pqcPub)))
		if err != nil {
			return Any{}, fmt.Errorf("proof of possession: %w", err)
		}
		b = putBytes(b, 6, pop)
	}
	return Any{TypeURL: TypeRegisterPQCKeyV2, Value: b}, nil
}

// ProofOfPossessionMessage is the text the PQC key signs to prove it is held.
func ProofOfPossessionMessage(chainID, sender string, pqcPub []byte) string {
	return fmt.Sprintf("qorechain-pqc-pop-v1|%s|%d|%s|%s", chainID, algorithmDilithium5, sender, hex.EncodeToString(pqcPub))
}

// pqcExtension is the PQCHybridSignature extension option carrying the
// ML-DSA-87 signature. pqc_public_key (field 3) is left out: the chain reads
// the key registered on the account.
func pqcExtension(sig []byte) Any {
	var b []byte
	b = putUvarint(b, 1, algorithmDilithium5)
	b = putBytes(b, 2, sig)
	return Any{TypeURL: typePQCHybridSig, Value: b}
}
