package vsro188

import (
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
)

func TestCOSMountAndLifecycleUIDCodecs(t *testing.T) {
	mount := network.EmptyClientPacket()
	mount.WriteByte(1)
	mount.WriteUInt32(88)
	request, err := (COSMountStateRequestCodec{}).Decode(mount)
	if err != nil || !request.Mounted || request.TransportUniqueID != 88 {
		t.Fatalf("mount Decode() = %+v, %v", request, err)
	}
	uid := network.EmptyClientPacket()
	uid.WriteUInt32(88)
	lifecycle, err := (COSUIDRequestCodec{}).Decode(uid)
	if err != nil || lifecycle.UniqueID != 88 {
		t.Fatalf("lifecycle Decode() = %+v, %v", lifecycle, err)
	}
	for _, malformed := range []network.Packet{
		func() network.Packet { p := network.EmptyClientPacket(); p.WriteByte(2); p.WriteUInt32(88); return p }(),
		func() network.Packet { p := network.EmptyClientPacket(); p.WriteByte(1); p.WriteUInt32(0); return p }(),
		func() network.Packet { p := network.EmptyClientPacket(); p.WriteUInt32(88); p.WriteByte(0); return p }(),
	} {
		if _, err := (COSMountStateRequestCodec{}).Decode(malformed); err == nil {
			t.Fatalf("mount Decode() accepted %x", malformed.Payload())
		}
	}
}

func TestCOSCommandCodecPreservesCommandOnFailure(t *testing.T) {
	attack := network.EmptyClientPacket()
	attack.WriteUInt32(88)
	attack.WriteByte(COSCommandAttack)
	attack.WriteUInt32(99)
	request, err := (COSCommandRequestCodec{}).Decode(attack)
	if err != nil || request.PetUniqueID != 88 || request.TargetUniqueID != 99 || request.SkillID != 0 {
		t.Fatalf("attack Decode() = %+v, %v", request, err)
	}
	cast := network.EmptyClientPacket()
	cast.WriteUInt32(88)
	cast.WriteByte(COSCommandCastSkill)
	cast.WriteUInt32(3824)
	cast.WriteUInt32(99)
	request, err = (COSCommandRequestCodec{}).Decode(cast)
	if err != nil || request.SkillID != 3824 || request.TargetUniqueID != 99 {
		t.Fatalf("cast Decode() = %+v, %v", request, err)
	}
	malformed := network.EmptyClientPacket()
	malformed.WriteUInt32(88)
	malformed.WriteByte(COSCommandCastSkill)
	malformed.WriteUInt32(3824)
	request, err = (COSCommandRequestCodec{}).Decode(malformed)
	if err == nil || request.Command != COSCommandCastSkill {
		t.Fatalf("malformed Decode() = %+v, %v", request, err)
	}
}
