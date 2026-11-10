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

/** Returns the options of Cesium3DTileset.fromUrl for a splat tileset. */
export function splatTilesetOptions() {
  return { ...tilesetOptions };
}

/**
 * Lowers the detail of a splat tileset, at most once a second, while its
 * memory is over the cache budget.
 */
export function installMemoryGuard(tileset) {
  let lastAdjustment = 0;
  const relax = () => {
    const now = performance.now();
    if (now - lastAdjustment < 1000 || tileset.totalMemoryUsageInBytes <= memoryLimit) {
      return;
    }
    lastAdjustment = now;
    tileset.maximumScreenSpaceError = Math.min(
      largestScreenSpaceError,
      Math.ceil(tileset.maximumScreenSpaceError * 1.35),
    );
    tileset.trimLoadedTiles();
    console.warn(
      `The splats are over the memory budget: the maximum screen space error is now ${tileset.maximumScreenSpaceError}.`,
    );
  };
  tileset.tileLoad.addEventListener(relax);
  tileset.tileUnload.addEventListener(relax);
}
