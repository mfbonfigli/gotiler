// Splat mode: Gaussian splat tilesets load with options that keep their
// memory in check, as large scenes otherwise take gigabytes in the browser.
// The options and the memory guard come from gotiler-desktop.

const megabyte = 1024 * 1024;

const tilesetOptions = {
  cacheBytes: 192 * megabyte,
  maximumCacheOverflowBytes: 96 * megabyte,
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

const memoryLimit = tilesetOptions.cacheBytes + tilesetOptions.maximumCacheOverflowBytes;
const largestScreenSpaceError = 160;
const errorStep = 1.35;

/** Returns the options of Cesium3DTileset.fromUrl for a splat tileset. */
export function splatTilesetOptions() {
  return { ...tilesetOptions };
}

/**
 * Lowers the detail of a splat tileset, at most once a second, while its
 * memory is over the cache budget, and raises it back toward the starting
 * detail once the memory falls under half the budget. The gap between the
 * two keeps the detail from going back and forth.
 */
export function installMemoryGuard(tileset) {
  const startingError = tileset.maximumScreenSpaceError;
  let lastAdjustment = 0;
  const adjust = () => {
    const now = performance.now();
    if (now - lastAdjustment < 1000) {
      return;
    }
    const memory = tileset.totalMemoryUsageInBytes;
    const error = tileset.maximumScreenSpaceError;
    if (memory > memoryLimit && error < largestScreenSpaceError) {
      tileset.maximumScreenSpaceError = Math.min(largestScreenSpaceError, Math.ceil(error * errorStep));
      tileset.trimLoadedTiles();
    } else if (memory < memoryLimit / 2 && error > startingError) {
      tileset.maximumScreenSpaceError = Math.max(startingError, Math.floor(error / errorStep));
    } else {
      return;
    }
    lastAdjustment = now;
    console.info(`Splat memory ${Math.round(memory / 1048576)} MB: the maximum screen space error is now ${tileset.maximumScreenSpaceError}.`);
  };
  tileset.tileLoad.addEventListener(adjust);
  tileset.tileUnload.addEventListener(adjust);
  tileset.allTilesLoaded.addEventListener(adjust);
}
