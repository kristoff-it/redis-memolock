import redis
from nltk import PorterStemmer

stemmer = PorterStemmer()
server = redis.Redis()

# This could very well be a serverless function 

def run(name: str):
    # Compute the result
    stem = stemmer.stem(name)

    # Write & publish on Redis
    server.set(f"ext:{name}", stem, ex=10)
    server.publish(f"ext/notif:{name}", stem)
    return stem


if __name__ == "__main__":
    import sys
    assert len(sys.argv) == 2, "Usage: python stemmer.py word"
    print(run(sys.argv[1]))
