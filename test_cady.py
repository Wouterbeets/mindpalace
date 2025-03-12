import ctranslate2
import sys

print(f"ctranslate2 version: {ctranslate2.__version__}", file=sys.stderr)
try:
    # Attempt to create a generator with CUDA
    generator = ctranslate2.Generator("facebook/m2m100_418M", device="cuda")
    print("CUDA is supported and working!", file=sys.stderr)
except Exception as e:
    print(f"CUDA test failed: {e}", file=sys.stderr)
