// Gaussian splat tilesets load with options that keep their memory in check,
// as large scenes otherwise take gigabytes in the browser. The options and
// the memory guard are those of gotiler view, which took them from
// gotiler-desktop.

const megabyte = 1024 * 1024;

export const splatTilesetOptions = {
  skipLevelOfDetail: true,
  immediatelyLoadDesiredLevelOfDetail: true,
  loadSiblings: false,
  preferLeaves: false,
  preloadWhenHidden: false,
  preloadFlightDestinations: false,
  cullRequestsWhileMoving: true,
  cullRequestsWhileMovingMultiplier: 120,
  foveatedScreenSpaceError: true,
  foveatedConeSize: 0.05,
  foveatedMinimumScreenSpaceErrorRelaxation: 16,
  foveatedTimeDelay: 0.6,
  dynamicScreenSpaceError: true,
  dynamicScreenSpaceErrorFactor: 32,
};

const largestScreenSpaceError = 160;
const errorStep = 1.35;

/**
 * Lowers the detail of a splat tileset, at most once a second, while its
 * memory is over the budget, and raises it back toward the chosen detail
 * once the memory falls under half the budget. The gap between the two keeps
 * the detail from going back and forth. Returns a function that sets the
 * chosen detail, as a maximum screen-space error.
 */
export function installMemoryGuard(tileset, startingError) {
  let chosenError = startingError;
  let lastAdjustment = 0;
  const adjust = () => {
    const now = performance.now();
    if (now - lastAdjustment < 1000) {
      return;
    }
    const limit = tileset.cacheBytes + tileset.maximumCacheOverflowBytes;
    const memory = tileset.totalMemoryUsageInBytes;
    const error = tileset.maximumScreenSpaceError;
    if (memory > limit && error < largestScreenSpaceError) {
      tileset.maximumScreenSpaceError = Math.min(largestScreenSpaceError, Math.ceil(error * errorStep));
      tileset.trimLoadedTiles();
    } else if (memory < limit / 2 && error > chosenError) {
      tileset.maximumScreenSpaceError = Math.max(chosenError, Math.floor(error / errorStep));
    } else {
      return;
    }
    lastAdjustment = now;
    console.info(`Splat memory ${Math.round(memory / megabyte)} MB: the maximum screen-space error is now ${tileset.maximumScreenSpaceError}.`);
  };
  tileset.tileLoad.addEventListener(adjust);
  tileset.tileUnload.addEventListener(adjust);
  tileset.allTilesLoaded.addEventListener(adjust);
  return (error) => {
    chosenError = error;
    tileset.maximumScreenSpaceError = error;
  };
}
