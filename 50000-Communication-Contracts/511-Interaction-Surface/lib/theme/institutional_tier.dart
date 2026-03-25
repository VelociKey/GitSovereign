import 'package:flutter/material.dart';

enum InstitutionalTier {
  solo,
  team,
  seed,
  beyond,
  whiteLabel,
}

class MaturityPalette {
  final Color primarySeed;
  final Color secondarySeed;
  final Color tertiarySeed;

  const MaturityPalette({
    required this.primarySeed,
    required this.secondarySeed,
    required this.tertiarySeed,
  });

  static const Map<InstitutionalTier, MaturityPalette> registry = {
    InstitutionalTier.solo: MaturityPalette(
      primarySeed: Color(0xFF00FF41), // Matrix Green
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF003300),
    ),
    InstitutionalTier.team: MaturityPalette(
      primarySeed: Color(0xFF00A3FF), // Sky Blue
      secondarySeed: Color(0xFFFFFFFF),
      tertiarySeed: Color(0xFF002244),
    ),
    InstitutionalTier.seed: MaturityPalette(
      primarySeed: Color(0xFF001F3F), // Imperial Navy
      secondarySeed: Color(0xFFE5E4E2), // Platinum
      tertiarySeed: Color(0xFF3D2B1F), // Deep Umber
    ),
    InstitutionalTier.beyond: MaturityPalette(
      primarySeed: Color(0xFFFFD700), // Gold
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF331100),
    ),
    InstitutionalTier.whiteLabel: MaturityPalette(
      primarySeed: Color(0xFFFFFFFF), // Custom Injection
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF808080),
    ),
  };
}
