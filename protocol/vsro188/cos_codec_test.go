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

func TestCOSCommandCodecDecodesStockMovePickupAndFollow(t *testing.T) {
	move := network.EmptyClientPacket()
	move.WriteUInt32(88)
	move.WriteByte(COSCommandMove)
	move.WriteByte(1)
	move.WriteUInt16(25000)
	move.WriteUInt16(100)
	move.WriteUInt16(20)
	move.WriteUInt16(300)
	request, err := (COSCommandRequestCodec{}).Decode(move)
	if err != nil || !request.Movement.HasDestination || request.Movement.Destination.RegionID != 25000 || request.Movement.Destination.X != 100 || request.Movement.Destination.Y != 20 || request.Movement.Destination.Z != 300 {
		t.Fatalf("move Decode() = %+v, %v", request, err)
	}
	pickup := network.EmptyClientPacket()
	pickup.WriteUInt32(88)
	pickup.WriteByte(COSCommandPickup)
	pickup.WriteUInt32(99)
	request, err = (COSCommandRequestCodec{}).Decode(pickup)
	if err != nil || request.TargetUniqueID != 99 {
		t.Fatalf("pickup Decode() = %+v, %v", request, err)
	}
	follow := network.EmptyClientPacket()
	follow.WriteUInt32(88)
	follow.WriteByte(COSCommandFollow)
	request, err = (COSCommandRequestCodec{}).Decode(follow)
	if err != nil || request.Command != COSCommandFollow {
		t.Fatalf("follow Decode() = %+v, %v", request, err)
	}
}

func TestCOSNameAndSettingsCodecs(t *testing.T) {
	name := network.EmptyClientPacket()
	name.WriteUInt32(88)
	name.WriteString("Wolf")
	nameRequest, err := (COSNameRequestCodec{}).Decode(name)
	if err != nil || nameRequest.PetUniqueID != 88 || nameRequest.Name != "Wolf" {
		t.Fatalf("name Decode() = %+v, %v", nameRequest, err)
	}
	settings := network.EmptyClientPacket()
	settings.WriteUInt32(88)
	settings.WriteByte(1)
	settings.WriteUInt32(0x11)
	settingsRequest, err := (COSSettingsRequestCodec{}).Decode(settings)
	if err != nil || settingsRequest.PetUniqueID != 88 || settingsRequest.Setting != 1 || settingsRequest.Settings != 0x11 {
		t.Fatalf("settings Decode() = %+v, %v", settingsRequest, err)
	}
}
