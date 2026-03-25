import 'package:flutter/material.dart';

enum MaturityTier {
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

  static const Map<MaturityTier, MaturityPalette> registry = {
    MaturityTier.solo: MaturityPalette(
      primarySeed: Color(0xFF00FF41), // Matrix Green
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF003300),
    ),
    MaturityTier.team: MaturityPalette(
      primarySeed: Color(0xFF00A3FF), // Sky Blue
      secondarySeed: Color(0xFFFFFFFF),
      tertiarySeed: Color(0xFF002244),
    ),
    MaturityTier.seed: MaturityPalette(
      primarySeed: Color(0xFF1A237E), // Imperial Navy (M3 Compliant)
      secondarySeed: Color(0xFFE5E4E2), // Platinum
      tertiarySeed: Color(0xFF5D4037), // Deep Umber (M3 Compliant)
    ),
    MaturityTier.beyond: MaturityPalette(
      primarySeed: Color(0xFFFFD700), // Gold
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF331100),
    ),
    MaturityTier.whiteLabel: MaturityPalette(
      primarySeed: Color(0xFFFFFFFF), // Custom Injection Slot
      secondarySeed: Color(0xFF000000),
      tertiarySeed: Color(0xFF808080),
    ),
  };
}
